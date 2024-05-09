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
		invoice := strings.ReplaceAll(form.Value["invoice"][0], " ", "")
		tags := map[string]string{
			"invoice":  invoice,
			"lastName": strings.ReplaceAll(form.Value["lastName"][0], " ", ""),
			"lot":      strings.ReplaceAll(form.Value["lot"][0], " ", ""),
		}

		// Loop through all files in data form
		for name, files := range form.File {
			// open every file and upload
			for _, fileHeader := range files {
				if fileHeader.Size > 6*1024*1024 {
					c.String(http.StatusBadRequest, "File Size Must Not Exceed 6 MB")
					break
				}

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
