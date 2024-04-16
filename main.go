package main

import (
	"context"
	"log"
	"os"

	"github.com/cccrizzz/ccpd-gin-server/contactUs"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoClient *mongo.Client
func initMongo() {
	// connect mongo db
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

func main() {
	// load dotenv
	err := godotenv.Load()
	if err != nil {
	  log.Fatal("Cannot find .env file")
	}

	// call init mongo and create collection object
	initMongo()
	contactMessegesCollection := mongoClient.Database("CCPD").Collection("ContactMesseges")
	
	// active release mode
	// gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.POST("/submitContactForm", contactUs.SubmitContactForm(contactMessegesCollection))
	r.GET("/getContactForm", contactUs.GetContactForm(contactMessegesCollection))
	r.Run(":3000")
}

