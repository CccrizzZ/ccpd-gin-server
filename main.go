package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cccrizzz/ccpd-gin-server/pkg/appointment"
	"github.com/cccrizzz/ccpd-gin-server/pkg/contact"
	"github.com/cccrizzz/ccpd-gin-server/pkg/whitelist"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	middleware "github.com/s12i/gin-throttle"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Mongo DB
var mongoClient *mongo.Client

func initMongo() {
	uri := os.Getenv("MONGO_CONN")
	if uri == "" {
		log.Fatal("Environment variable not found.")
	}
	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	mongoClient = client
}

// IP whitelist
var IPList = map[string]bool{
	"127.0.0.1":      true,
	"142.114.216.52": true,
}

func main() {
	// load dotenv
	godotenv.Load()

	// call init mongo and create collection object
	initMongo()
	contactMessegesCollection := mongoClient.Database("CCPD").Collection("ContactMesseges")
	appointmentLinksCollection := mongoClient.Database("CCPD").Collection("AppointmentLinks")

	// active release mode
	if os.Getenv("MODE") == "" || os.Getenv("MODE") == "DEBUG" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	// throttle middleware
	maxEventsPerSec := 10
	maxBurstSize := 2
	r.Use(middleware.Throttle(maxEventsPerSec, maxBurstSize))
	// ip whitelist middleware
	r.Use(whitelist.IPWhiteListMiddleware(IPList))
	// cors middleware
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"PUT", "PATCH", "OPTION", "GET", "POST"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return origin == "*"
		},
		MaxAge: 12 * time.Hour,
	}))

	// contact form
	r.POST("/submitContactForm", contact.SubmitContactForm(contactMessegesCollection))
	r.POST("/getContactFormByPage", contact.GetContactFormByPage(contactMessegesCollection))
	r.POST("/setContactFormReplied", contact.SetContactFormReplied(contactMessegesCollection))

	// appointment links
	r.GET("/getAppointmentLink", appointment.GetAppointmentLink(appointmentLinksCollection))
	r.POST("/setAppointmentLink", appointment.SetAppointmentLink(appointmentLinksCollection))
	r.Run(":3000")
}
