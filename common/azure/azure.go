package azure

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/gin-gonic/gin"
)

func InitAzureServiceClient() *container.Client {
	connUrl := os.Getenv("AZURE_URL")

	// option remains empty
	clientOptions := azblob.ClientOptions{}

	// Create a service client using the SAS token
	serviceClient, err := azblob.NewClientWithNoCredential(connUrl, &clientOptions)
	if err != nil {
		fmt.Println(err.Error())
		log.Fatal("Cannot create azure service client")
	}
	containerClient := serviceClient.ServiceClient().NewContainerClient("contact-image")
	return containerClient
}

func SubmitImages(containerClient *container.Client) gin.HandlerFunc {
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
			"email":    form.Value["email"][0],
			"invoice":  invoice,
			"lastName": form.Value["lastName"][0],
			"lot":      form.Value["lot"][0],
		}
		fmt.Println(tags)

		// Loop through the files
		for name, files := range form.File {
			fmt.Println(files)
			fmt.Println(name)

			// open every file and upload
			for _, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					log.Fatal("Cannot Open File")
				}

				blobClient := containerClient.NewBlockBlobClient(invoice + "/" + fileHeader.Filename)
				// _, err = blobClient.SetTags(ctx, tags, nil)
				// if err != nil {
				// 	fmt.Println(err.Error())
				// 	log.Println("Error setting tags:", err)
				// 	file.Close()
				// 	continue
				// }
				_, err = blobClient.Upload(ctx, file, nil)
				if err != nil {
					log.Println("Error uploading file:", err)
					file.Close()
					continue
				}

				// set tags
				file.Close()
			}
		}
	}
}
