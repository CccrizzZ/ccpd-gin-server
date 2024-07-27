package do

import (
	"log"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// digital ocean space object storage connector
func InitSpaceObjectStorage() *minio.Client {
	// digital ocean object storage
	accessKey := os.Getenv("SPACE_KEY")
	accessSecret := os.Getenv("SPACE_SECRET")
	endpoint := os.Getenv("STORAGE_ENDPOINT")
	ssl := true
	client, err := minio.New(
		endpoint,
		&minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, accessSecret, ""),
			Secure: ssl,
			Region: "us-east-1",
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	return client
}
