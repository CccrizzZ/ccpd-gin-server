package pcontent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"

	// "github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// var validate = validator.New()

type PageContent struct {
	VideoLink       string `json:"link" binding:"required" validate:"required"`
	BookingLink     string `json:"bookingLink" binding:"required" validate:"required"`
	BannerImageName string `json:"BannerLink" binding:"required" validate:"required"`
}

func GetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// pull settings from mongo
		var result bson.M
		err := collection.FindOne(
			context.TODO(),
			bson.M{},
			nil,
		).Decode(&result)
		if err != nil {
			fmt.Println(err.Error())
			c.String(http.StatusNotFound, "Item Not Found!")
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// page content is an array of ContentInfo
type ContentInfo struct {
	Name string            `json:"name" binding:"required" validate:"required"`
	Data map[string]string `json:"data"`
}

func SetPageContent(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind json
		var body []ContentInfo

		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr)
			c.String(404, "Please Check Your Inputs!")
			return
		}

		// update current page content
		setMsg, err := collection.UpdateOne(
			ctx,
			bson.M{"type": "setting"},
			bson.M{
				"$set": bson.M{
					"contentObj": body,
				},
			},
		)
		if err != nil {
			c.String(500, "Cannot Update Document")
			return
		}

		c.JSON(http.StatusOK, setMsg)
	}
}

// get all 258.ca assets url
func GetAllAssetsUrlArr(storageClient *minio.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var urlArr []string

		bucketName := "crm-258-storage"
		// check if bucket exist in object storage
		exists, existErr := storageClient.BucketExists(ctx, bucketName)
		if existErr != nil || !exists {
			// fmt.Println(existErr.Error())
			c.String(500, "No Such Bucket")
			return
		}

		// list all objects
		// prefix := "258Assets/"
		for object := range storageClient.ListObjects(
			ctx,
			bucketName,
			minio.ListObjectsOptions{
				Recursive: true,
				// Prefix:    prefix,
			},
		) {
			// if object.Key == prefix {
			// 	continue
			// }
			presignedURL, err := storageClient.PresignedGetObject(
				ctx,
				bucketName,
				object.Key,
				time.Hour*24,
				nil,
			)
			if err != nil {
				fmt.Println(err.Error())
				c.String(500, "Error Getting Objects")
				return
			}
			urlArr = append(urlArr, presignedURL.String())
		}
		c.JSON(200, gin.H{"arr": urlArr})
	}
}

// for uploading page assets only
func UploadPageAsset(storageClient *minio.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		form, err := c.MultipartForm()
		if err != nil {
			fmt.Println("Cannot Open File:", err)
			c.String(400, "Invalid Body")
			return
		}

		// loop all files
		files := form.File["files"]
		for _, file := range files {
			fh, err := file.Open()
			if err != nil {
				fmt.Println(err)
				c.String(400, "Invalid Body")
				fh.Close()
				return
			}
			defer fh.Close()

			// check if bucket exist
			bucketName := "crm-258-storage"
			exists, existErr := storageClient.BucketExists(ctx, bucketName)
			if existErr != nil || !exists {
				c.String(500, "No Such Bucket")
				return
			}

			// upload pdf file to space object storage
			_, uploadErr := storageClient.PutObject(
				ctx,
				bucketName,
				file.Filename,
				fh,
				file.Size,
				minio.PutObjectOptions{
					ContentType: file.Header.Get("Content-Type"),
					UserMetadata: map[string]string{
						"x-amz-acl": "public-read",
					},
				},
			)
			if uploadErr != nil {
				c.String(500, "Failed to Upload %s", uploadErr.Error())
				return
			}
		}
		c.String(200, "Upload Success")
	}
}

// could be use to delete both page assets and contact images
func DeleteAssetByName(storageClient *minio.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			FileName string `json:"fileName"`
		}
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.String(400, "Invalid Body")
			return
		}

		// fmt.Println(req.FileName)
		storageClient.RemoveObject(
			context.TODO(),
			"crm-258-storage",
			req.FileName,
			minio.RemoveObjectOptions{},
		)

		c.String(200, "Successfully Deleted")
	}
}
