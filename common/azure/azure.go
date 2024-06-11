package azure

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/gin-gonic/gin"
)

func InitAzureServiceClient() *service.Client {
	connUrl := os.Getenv("AZURE_URL")

	// option remains empty
	clientOptions := azblob.ClientOptions{}

	// Create a service client using the SAS token
	serviceClient, err := azblob.NewClientWithNoCredential(connUrl, &clientOptions)
	if err != nil {
		fmt.Println(err.Error())
		log.Fatal("Cannot create azure service client")
	}
	sClient := serviceClient.ServiceClient()
	return sClient
}

// submit contact-us form attached images
func SubmitImages(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// get files from form
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// construct tags
		// remove space
		invoice := strings.ReplaceAll(form.Value["invoice"][0], " ", "")
		lot := strings.ReplaceAll(form.Value["lot"][0], " ", "")
		tags := map[string]string{
			"invoice":  invoice,
			"lastName": strings.ReplaceAll(form.Value["lastName"][0], " ", ""),
			"lot":      strings.ReplaceAll(lot, ".", ""), // remove dots
		}

		// Loop through all files in data form
		for name, files := range form.File {
			// open every file and upload
			for _, fileHeader := range files {
				if fileHeader.Size > 6*1024*1024 {
					c.String(http.StatusBadRequest, "File Size Must Not Exceed 6 MB")
					break
				}

				// open file
				file, err := fileHeader.Open()
				if err != nil {
					log.Println("Cannot Open File:", err)
					file.Close()
				}

				// init client
				containerClient := serviceClient.NewContainerClient("contact-image" + "/" + invoice)
				blobClient := containerClient.NewBlockBlobClient(name)

				// upload the blob to destination
				_, err = blobClient.Upload(ctx, file, nil)
				if err != nil {
					log.Println("Error uploading file:", err)
					file.Close()
					break
				}

				// set tags to blob
				_, err = blobClient.SetTags(ctx, tags, nil)
				if err != nil {
					log.Println("Error setting tags:", err)
					file.Close()
					break
				}
				file.Close()
			}
		}
	}
}

type ImageTagRequest struct {
	Invoice  string `json:"invoice" binding:"required" validate:"required"`
	LastName string `json:"lastName" binding:"required" validate:"required"`
	Lot      string `json:"lot" binding:"required" validate:"required"`
}

// view image in the 258 admin console
func GetImagesUrlsByTag(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var body ImageTagRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// construct tags
		filterString := "\"invoice\"='" + body.Invoice + "'and\"lastName\"='" + body.LastName + "'and\"lot\"='" + body.Lot + "'"

		// make new container client
		containerClient := serviceClient.NewContainerClient("contact-image")
		filtered, err := containerClient.FilterBlobs(ctx, filterString, nil)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		// result array
		var urlArr []string

		if len(filtered.Blobs) == 0 {
			c.String(http.StatusNotFound, "No Photos Found!")
			return
		}

		// loop filered blobs
		for _, blob := range filtered.Blobs {
			bClient := containerClient.NewBlobClient(*blob.Name)
			urlArr = append(urlArr, bClient.URL())
		}

		c.JSON(http.StatusOK, gin.H{"data": urlArr})
	}
}

// upload single image to page content images blob container
func UploadPageContentAssets(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// read file from body
		form, err := c.MultipartForm()
		if err != nil {
			fmt.Println("Cannot Open File:", err)
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		files := form.File["files"]
		for _, file := range files {

			// // file size should be under 10mb
			// if file.Size > 10*1024*1024 {
			// 	c.String(http.StatusBadRequest, "File Must Be Under 10 MB")
			// 	return
			// }

			// open the file
			fh, err := file.Open()
			if err != nil {
				fmt.Println("Cannot Open File:", err)
				c.String(http.StatusBadRequest, "Invalid Body")
				fh.Close()
				return
			}

			// create blob client
			containerClient := serviceClient.NewContainerClient("page-content-image")
			blobClient := containerClient.NewBlockBlobClient(file.Filename)

			// upload the blob
			_, err = blobClient.Upload(ctx, fh, nil)
			if err != nil {
				fmt.Println("Error Uploading File:", err)
				c.String(http.StatusInternalServerError, "Error Uploading File")
				fh.Close()
				return
			}
		}
		c.String(http.StatusOK, "Uploaded Image")
	}
}

type DeleteRequest struct {
	FileName string `json:"fileName" binding:"required" validate:"required"`
}

func DeletePageContentAssetByName(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ctx := context.Background()
		var body DeleteRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
		}

	}
}

// called by 258 web app to load page content image
func GetAssetsUrlArr(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var urlArr []string

		// create blob client
		containerClient := serviceClient.NewContainerClient("page-content-image")
		pager := containerClient.NewListBlobsFlatPager(nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				c.String(http.StatusNotFound, "Cannot Get Assets Gallery")
			}

			for _, blobInfo := range page.Segment.BlobItems {
				blobUrl := containerClient.NewBlobClient(*blobInfo.Name).URL()
				cleanURL := strings.Split(blobUrl, "?")[0]
				urlArr = append(urlArr, cleanURL)
			}
		}

		c.JSON(http.StatusOK, gin.H{"arr": urlArr})
	}
}
