package storage

import (
	"bytes"
	"io/ioutil"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type S3Storage struct {
	S3session *s3.S3
	AccessKey string
	SecretKey string
	// Endpoint  string
	Region string
	Bucket string
}

type s3Implement struct {
	s3storage *S3Storage
}

type S3 interface {
	PutObject(bucket, key string, data []byte) error
	HeadObject(bucket, key string) (bool, error)
	GetObject(bucket, key string) ([]byte, error)
	GetObjectPresignUrl(bucket, key string) (string, error)
}

func (storage *S3Storage) NewS3() {
	credentials := credentials.NewStaticCredentials(storage.AccessKey, storage.SecretKey, "")
	_, err := credentials.Get()
	if err != nil {
		log.Fatal(err)
	}
	storage.S3session = s3.New(session.Must(session.NewSession(&aws.Config{
		Credentials: credentials,
		// Endpoint:    aws.String(storage.Endpoint),
		Region: aws.String(storage.Region),
	})))
}

func NewImplementS3(s3storage *S3Storage) S3 {
	return &s3Implement{
		s3storage: s3storage,
	}
}

func (s *s3Implement) PutObject(bucket, key string, data []byte) error {
	_, err := s.s3storage.S3session.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *s3Implement) HeadObject(bucket, key string) (bool, error) {
	_, err := s.s3storage.S3session.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if aerr, ok := err.(awserr.Error); ok {
		if aerr.Code() == "NotFound" {
			return false, err
		}
	}
	return true, nil
}

func (s *s3Implement) GetObject(bucket, key string) ([]byte, error) {
	obj, err := s.s3storage.S3session.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer obj.Body.Close()
	body, err := ioutil.ReadAll(obj.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *s3Implement) GetObjectPresignUrl(bucket, key string) (string, error) {
	req, _ := s.s3storage.S3session.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	url, err := req.Presign(15 * time.Minute)
	if err != nil {
		return "", err
	}
	return url, nil
}
