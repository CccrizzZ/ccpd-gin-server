package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/cccrizzz/ccpd-gin-server/contactUs"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	middleware "github.com/s12i/gin-throttle"
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
	
	// throttle middleware
	maxEventsPerSec := 10
	maxBurstSize := 2
	r.Use(middleware.Throttle(maxEventsPerSec, maxBurstSize))

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

	r.POST("/submitContactForm", contactUs.SubmitContactForm(contactMessegesCollection))
	r.GET("/getContactForm", contactUs.GetContactForm(contactMessegesCollection))
	r.Run(":3000")
}

// func CORSMiddleware() gin.HandlerFunc {
//     return func(c *gin.Context) {
//         c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
//         c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
//         c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
//         c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

//         if c.Request.Method == "OPTIONS" {
//             c.AbortWithStatus(204)
//             return
//         }

//         c.Next()
//     }
// }