package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cccrizzz/ccpd-gin-server/common/azure"
	"github.com/cccrizzz/ccpd-gin-server/common/do"
	auth "github.com/cccrizzz/ccpd-gin-server/common/firebase"
	"github.com/cccrizzz/ccpd-gin-server/common/mongo"
	"github.com/cccrizzz/ccpd-gin-server/pkg/contact"
	"github.com/cccrizzz/ccpd-gin-server/pkg/invoices"
	appointment "github.com/cccrizzz/ccpd-gin-server/pkg/pcontent"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	middleware "github.com/s12i/gin-throttle"
)

// IP whitelist
// var IPList = map[string]bool{
// 	"127.0.0.1":      true,
// 	"142.114.216.52": true,
// }

func main() {
	// load dotenv
	godotenv.Load()

	// mongodb collections
	mongoClient := mongo.InitMongo()
	contactMessegesCollection := mongoClient.Database("CCPD").Collection("ContactMesseges")
	pageContenCollection := mongoClient.Database("CCPD").Collection("PageContent")
	invoicesCollection := mongoClient.Database("CCPD").Collection("Invoices_Production")
	remainingCollection := mongoClient.Database("CCPD").Collection("RemainingHistory")

	// digital ocean space object storage
	spaceObjectStorageClient := do.InitSpaceObjectStorage()

	// azure service client
	azureClient := azure.InitAzureServiceClient()

	// active release mode
	if os.Getenv("MODE") == "" || os.Getenv("MODE") == "DEBUG" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// create router
	r := gin.Default()

	// throttle middleware
	maxEventsPerSec := 10
	maxBurstSize := 5
	r.Use(middleware.Throttle(maxEventsPerSec, maxBurstSize))

	// ip whitelist middleware
	// r.Use(whitelist.IPWhiteListMiddleware(IPList))

	// trusted proxies
	r.ForwardedByClientIP = true
	r.SetTrustedProxies(nil)

	// cors middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"PUT", "PATCH", "OPTION", "GET", "POST", "DELETE"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return origin == "*"
		},
		MaxAge: 12 * time.Hour,
	}))

	// Initialize Firebase auth client once
	firebaseApp, err := auth.InitFirebase()
	if err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}

	// get firebase auth client
	firebaseAuthClient, err := firebaseApp.Auth(context.Background())
	if err != nil {
		log.Fatalf("Failed to get Firebase auth client: %v", err)
	}

	// use fb auth on all route
	// r.Use(auth.FirebaseAuthMiddleware(firebaseAuthClient))

	// contact form controller
	r.POST("/submitContactForm", contact.SubmitContactForm(contactMessegesCollection))
	r.POST("/submitImages", azure.SubmitImages(azureClient))
	r.POST("/GetImagesUrlsByTag", azure.GetImagesUrlsByTag(azureClient))
	r.POST("/getContactFormByPage", auth.FirebaseAuthMiddleware(firebaseAuthClient), contact.GetContactFormByPage(contactMessegesCollection))
	r.POST("/setContactFormReplied", auth.FirebaseAuthMiddleware(firebaseAuthClient), contact.SetContactFormReplied(contactMessegesCollection))

	// page content controller
	r.GET("/getPageContent", appointment.GetPageContent(pageContenCollection))
	r.POST("/setPageContent", auth.FirebaseAuthMiddleware(firebaseAuthClient), appointment.SetPageContent(pageContenCollection))
	r.GET("./getAssetsUrlArr", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.GetAssetsUrlArr(azureClient))
	r.POST("./uploadPageContentAssets", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.UploadPageContentAssets(azureClient))
	r.DELETE("./deletePageContentAsset", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.DeletePageContentAsset(azureClient))

	// invoices controller
	r.POST("/getInvoicesByPage", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.GetInvoicesByPage(invoicesCollection))
	r.POST("/createInvoiceFromPdf", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.CreateInvoiceFromPDF(spaceObjectStorageClient, remainingCollection))
	r.POST("/updateInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.UpdateInvoice(invoicesCollection))
	r.POST("/createInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.CreateInvoice(invoicesCollection))
	// r.POST("/convertAllTimes", invoices.ConvertAllTimes(invoicesCollection))

	r.Run(":3000")
}
