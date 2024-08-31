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
	pcontent "github.com/cccrizzz/ccpd-gin-server/pkg/pcontent"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	middleware "github.com/s12i/gin-throttle"
)

// IP whitelist
//
//	var IPList = map[string]bool{
//		"127.0.0.1":      true,
//		"142.114.216.52": true,
//	}

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
	maxEventsPerSec := 15
	maxBurstSize := 10
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

	// gorilla web socket
	r.GET("/ws", invoices.WsHandler)
	// go invoices.HandleBroadcasts()

	// contact form controller
	r.POST("/submitContactForm", contact.SubmitContactForm(contactMessegesCollection))
	// r.POST("/submitImages", azure.SubmitImages(azureClient))
	r.POST("/submitImages", contact.SubmitImages(spaceObjectStorageClient))
	// r.POST("/GetImagesUrlsByTag", azure.GetImagesUrlsByTag(azureClient))
	r.POST("/GetImagesUrlsByTag", contact.GetImagesUrlsByTag(spaceObjectStorageClient))
	r.POST("/getContactFormByPage", auth.FirebaseAuthMiddleware(firebaseAuthClient), contact.GetContactFormByPage(contactMessegesCollection))
	r.POST("/setContactFormReplied", auth.FirebaseAuthMiddleware(firebaseAuthClient), contact.SetContactFormReplied(contactMessegesCollection))

	// page content controller
	r.GET("/getPageContent", pcontent.GetPageContent(pageContenCollection))
	r.POST("/setPageContent", auth.FirebaseAuthMiddleware(firebaseAuthClient), pcontent.SetPageContent(pageContenCollection))
	r.GET("./getAssetsUrlArr", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.GetAssetsUrlArr(azureClient))
	r.POST("./uploadPageContentAssets", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.UploadPageContentAssets(azureClient))
	r.DELETE("./deletePageContentAsset", auth.FirebaseAuthMiddleware(firebaseAuthClient), azure.DeletePageContentAsset(azureClient))
	// digital ocean object store
	r.GET("./getAllAssetsUrlArr", auth.FirebaseAuthMiddleware(firebaseAuthClient), pcontent.GetAllAssetsUrlArr(spaceObjectStorageClient))
	r.PUT("./uploadPageAsset", auth.FirebaseAuthMiddleware(firebaseAuthClient), pcontent.UploadPageAsset(spaceObjectStorageClient))
	r.DELETE("./deleteAssetByName", auth.FirebaseAuthMiddleware(firebaseAuthClient), pcontent.DeleteAssetByName(spaceObjectStorageClient))

	// invoices controller
	r.POST("/getInvoicesByPage", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.GetInvoicesByPage(invoicesCollection))
	r.POST("/getInvoicesByInvoiceNumber", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.GetInvoiceByInvoiceNumber(invoicesCollection))
	r.POST("/createInvoiceFromPdf", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.CreateInvoiceFromPDF(spaceObjectStorageClient, remainingCollection))
	r.POST("/updateInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.UpdateInvoice(invoicesCollection))
	r.POST("/createInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.CreateInvoice(invoicesCollection))
	r.DELETE("/deleteInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.DeleteInvoice(invoicesCollection))
	r.POST("/uploadSignature", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.UploadSignature(spaceObjectStorageClient, invoicesCollection))
	r.GET("/getAllInvoiceLot", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.GetAllInvoiceLot(invoicesCollection))
	r.GET("/getChartData", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.GetChartData(invoicesCollection))
	r.POST("/confirmSignature", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.ConfirmSignature(invoicesCollection))
	r.DELETE("/deleteSignature", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.DeleteSignature(spaceObjectStorageClient, invoicesCollection))
	r.POST("/verifyInvoiceNumber", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.VerifyInvoiceNumber(invoicesCollection))
	r.POST("/refundInvoice", auth.FirebaseAuthMiddleware(firebaseAuthClient), invoices.RefundInvoice(invoicesCollection))
	// r.POST("/convertAllTimes", invoices.ConvertAllTimes(invoicesCollection))

	r.Run(":3000")
}
