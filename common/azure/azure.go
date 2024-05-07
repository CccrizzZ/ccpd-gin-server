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
	containerClient := serviceClient.ServiceClient()
	return containerClient
}

func sanitizeString(input string) string {
	sanitized := strings.ReplaceAll(input, " ", "_")
	sanitized = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return -1
	}, sanitized)

	return sanitized
}

// submit contact-us form attached images
func SubmitImages(serviceClient *service.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// get file from form
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// construct tags
		invoice := form.Value["invoice"][0]
		tags := map[string]string{
			"email":    sanitizeString(form.Value["email"][0]),
			"invoice":  sanitizeString(invoice),
			"lastName": sanitizeString(form.Value["lastName"][0]),
			"lot":      sanitizeString(form.Value["lot"][0]),
		}
		fmt.Println(tags)

		// Loop through all files in data form
		for name, files := range form.File {
			// open every file and upload
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					log.Fatal("Cannot Open File")
				}

				// init client
				containerClient := serviceClient.NewContainerClient("contact-image")
				blobClient := containerClient.NewBlockBlobClient(invoice + "/" + name)
				// blobClient := containerClient.NewBlobClient(invoice + "/" + name)

				// the specified blob does not exist
				// or
				// permission error, sas url switched

				// set tags
				_, err = blobClient.SetTags(ctx, tags, nil)
				if err != nil {
					fmt.Println(err.Error())
					log.Println("Error setting tags:", err)
					file.Close()
					continue
				}

				// upload the blob to destination
				_, err = blobClient.Upload(ctx, file, nil)
				if err != nil {
					log.Println("Error uploading file:", err)
					file.Close()
					continue
				}

				file.Close()
			}
		}
	}
}
