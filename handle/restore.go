package handle

import (
	"backup-chunk/cache"
	"backup-chunk/common"
	"backup-chunk/storage"
	supportos "backup-chunk/supportos/unix"
	"runtime"
	"strings"

	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type Download struct {
	Storage storage.S3
}

func (d *Download) Download(recoveryPointID string, destDir string) error {
	indexPath := filepath.Join(".cache", recoveryPointID, "index.json")
	indexKey := filepath.Join(recoveryPointID, "index.json")
	_, err := os.Stat(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Get %s from storage \n", indexKey)
			buf, err := d.Storage.GetObject(common.Bucket, indexKey)
			if err == nil {
				_ = os.MkdirAll(filepath.Join(".cache", recoveryPointID), 0700)
				if err := ioutil.WriteFile(indexPath, buf, 0700); err != nil {
					fmt.Printf("Error writing %s file %+v ", indexKey, err)
					return err
				}
			} else {
				fmt.Printf("Get %s from storage error %+v ", indexKey, err)
				return err
			}
		} else {
			fmt.Printf("Start %s file error %+v ", indexKey, err)
			return err
		}
	}

	index := cache.Index{}
	buf, err := ioutil.ReadFile(indexPath)
	if err != nil {
		fmt.Printf("Read %s error %+v ", indexKey, err)
		return err
	} else {
		_ = json.Unmarshal([]byte(buf), &index)
	}

	fmt.Printf("Download to directory %s \n", filepath.Clean(destDir))
	if err := d.restoreDirectory(index, filepath.Clean(destDir)); err != nil {
		fmt.Printf("Download file %s error %+v ", filepath.Clean(destDir), err)
		return err
	}

	return nil
}

func (d *Download) restoreDirectory(index cache.Index, destDir string) error {
	maxWorkers := runtime.NumCPU()
	sem := semaphore.NewWeighted(int64(maxWorkers))
	group, ctx := errgroup.WithContext(context.Background())

	for _, item := range index.Items {
		item := item
		err := sem.Acquire(ctx, 1)
		if err != nil {
			fmt.Printf("Acquire err = %+v\n", err)
			continue
		}

		// fmt.Printf("Checking item %s \n", item.AbsolutePath)
		group.Go(func() error {
			defer sem.Release(1)
			err := d.downloadItem(ctx, *item, destDir)
			if err != nil {
				fmt.Printf("Download item %s error %+v ", item.AbsolutePath, err)
				return err
			}
			return nil
		})

	}

	if err := group.Wait(); err != nil {
		fmt.Printf("Has goroutine error %+v\n", err)
		return err
	}

	fmt.Println("Finish restore")

	return nil
}

func (d *Download) downloadItem(ctx context.Context, item cache.Node, destDir string) error {
	var pathItem string
	if destDir == item.BasePath {
		pathItem = item.AbsolutePath
	} else {
		pathItem = filepath.Join(destDir, item.RelativePath)
	}
	switch item.Type {
	case "symlink":
		err := d.downloadSymlink(pathItem, item)
		if err != nil {
			fmt.Printf("Download symlink error %+v ", err)
			return err
		}
	case "dir":
		err := d.downloadDirectory(pathItem, item)
		if err != nil {
			fmt.Printf("Download directory error %+v ", err)
			return err
		}
	case "file":
		err := d.downloadFile(pathItem, item)
		if err != nil {
			fmt.Printf("Download file error %+v ", err)
			return err
		}
	}

	return nil
}

func (d *Download) downloadSymlink(pathItem string, item cache.Node) error {
	fi, err := os.Stat(pathItem)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Symlink not exist, create ", pathItem)
			err := d.createSymlink(item.LinkTarget, pathItem, item.Mode, int(item.UID), int(item.GID))
			if err != nil {
				return err
			}
			return nil
		} else {
			return err
		}
	} else {
		fmt.Printf("Symlink exist %s => check symlink \n", pathItem)
		_, ctimeLocal, _, _, _, _ := supportos.ItemLocal(fi)
		if !strings.EqualFold(common.TimeToString(ctimeLocal), common.TimeToString(item.ChangeTime)) {
			fmt.Printf("Symlink %s change ctime => update mode, uid, gid \n", item.Name)
			err = os.Chmod(pathItem, item.Mode)
			if err != nil {
				return err
			}
			_ = supportos.SetChownItem(pathItem, int(item.UID), int(item.GID))
		} else {
			fmt.Printf("Symlink %s not change => not download \n", pathItem)
		}
	}
	return nil
}

func (d *Download) downloadDirectory(pathItem string, item cache.Node) error {
	fi, err := os.Stat(pathItem)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Directory not exist => create ", pathItem)
			err := d.createDirectory(pathItem, os.ModeDir|item.Mode, int(item.UID), int(item.GID), item.AccessTime, item.ModTime)
			if err != nil {
				return err
			}
			return nil
		} else {
			return err
		}
	} else {
		fmt.Printf("Directory exist %s => check directory \n", pathItem)
		_, ctimeLocal, _, _, _, _ := supportos.ItemLocal(fi)
		if !strings.EqualFold(common.TimeToString(ctimeLocal), common.TimeToString(item.ChangeTime)) {
			fmt.Printf("Directory %s change ctime => update mode, uid, gid \n", item.Name)
			err = os.Chmod(pathItem, os.ModeDir|item.Mode)
			if err != nil {
				return err
			}
			_ = supportos.SetChownItem(pathItem, int(item.UID), int(item.GID))
		} else {
			fmt.Printf("Directory %s not change => not download \n", pathItem)
		}
	}
	return nil
}

func (d *Download) downloadFile(pathItem string, item cache.Node) error {
	fi, err := os.Stat(pathItem)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("File not exist => create ", pathItem)
			file, err := d.createFile(pathItem, item.Mode, int(item.UID), int(item.GID))
			if err != nil {
				return err
			}

			err = d.writeFile(file, item)
			if err != nil {
				return err
			}
			return nil
		} else {
			return err
		}
	} else {
		fmt.Printf("File exist %s => check file \n", pathItem)
		_, ctimeLocal, mtimeLocal, _, _, _ := supportos.ItemLocal(fi)
		if !strings.EqualFold(common.TimeToString(ctimeLocal), common.TimeToString(item.ChangeTime)) {
			if !strings.EqualFold(common.TimeToString(mtimeLocal), common.TimeToString(item.ModTime)) {
				fmt.Printf("File change mtime, ctime => create %s \n", pathItem)
				if err = os.Remove(pathItem); err != nil {
					return err
				}

				file, err := d.createFile(pathItem, item.Mode, int(item.UID), int(item.GID))
				if err != nil {
					return err
				}

				err = d.writeFile(file, item)
				if err != nil {
					return err
				}
				return nil
			} else {
				fmt.Printf("File %s change ctime => update mode, uid, gid \n", pathItem)
				err = os.Chmod(pathItem, item.Mode)
				if err != nil {
					return err
				}
				_ = supportos.SetChownItem(pathItem, int(item.UID), int(item.GID))
				err = os.Chtimes(pathItem, item.AccessTime, item.ModTime)
				if err != nil {
					return err
				}
			}
		} else {
			fmt.Printf("File %s not change => not download \n", pathItem)
		}
	}

	return nil
}

func (d *Download) writeFile(file *os.File, item cache.Node) error {
	for _, info := range item.Content {

		offset := info.Start
		key := info.Etag

		fmt.Printf("Download object %s of %s \n", key, item.AbsolutePath)
		data, err := d.Storage.GetObject(common.Bucket, key)
		if err != nil {
			return err
		}

		_, err = file.WriteAt(data, int64(offset))
		if err != nil {
			fmt.Printf("Write file error %+v", err)
			return err
		}
	}

	err := os.Chmod(file.Name(), item.Mode)
	if err != nil {
		return err
	}
	_ = supportos.SetChownItem(file.Name(), int(item.UID), int(item.GID))
	err = os.Chtimes(file.Name(), item.AccessTime, item.ModTime)
	if err != nil {
		return err
	}

	return nil
}

func (d *Download) createSymlink(symlinkPath string, path string, mode fs.FileMode, uid int, gid int) error {
	dirName := filepath.Dir(path)
	if _, err := os.Stat(dirName); os.IsNotExist(err) {
		if err := os.MkdirAll(dirName, os.ModePerm); err != nil {
			return err
		}
	}

	_ = os.Symlink(symlinkPath, path)

	_ = os.Chmod(path, mode)

	_ = supportos.SetChownItem(path, uid, gid)

	return nil
}

func (d *Download) createDirectory(path string, mode fs.FileMode, uid int, gid int, atime time.Time, mtime time.Time) error {
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return err
	}

	err = os.Chmod(path, mode)
	if err != nil {
		return err
	}

	_ = supportos.SetChownItem(path, uid, gid)
	err = os.Chtimes(path, atime, mtime)
	if err != nil {
		return err
	}

	return nil
}

func (d *Download) createFile(path string, mode fs.FileMode, uid int, gid int) (*os.File, error) {
	dirName := filepath.Dir(path)
	if _, err := os.Stat(dirName); os.IsNotExist(err) {
		if err := os.MkdirAll(dirName, 0700); err != nil {
			return nil, err
		}
	}
	var file *os.File
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	err = os.Chmod(path, mode)
	if err != nil {
		return nil, err
	}

	_ = supportos.SetChownItem(path, uid, gid)

	return file, nil
}
