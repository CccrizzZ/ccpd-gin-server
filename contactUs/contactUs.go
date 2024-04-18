package contactUs

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type ContactUsForm struct {
    FirstName string `json:"firstname" binding:"required"`
    LastName string `json:"lastname" binding:"required"`
    Phone  string `json:"phone" binding:"required"`
    Email  string `json:"email" binding:"required"`
	Invoice string `json:"invoice" binding:"required"`
	Lot string `json:"lot" binding:"required"`
	Reason string `json:"reason" binding:"required"`
	Message string `json:"message" binding:"required"`
	Time string `json:"time" binding:"required"`
}

type Response struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func SubmitContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// bind incoming json to struct
		var newFormObj ContactUsForm
		bindErr := c.ShouldBindJSON(&newFormObj)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Cannot Bind JSON, Request Must Match Schema")
			return
		}

		// generate EST time
		currTime, err := time.LoadLocation("America/New_York")
		if err != nil {
			c.String(http.StatusInternalServerError,  "Cannot Get Eastern Standard Time")
			return
		}
		fmt.Println(newFormObj)
		fmt.Println(currTime)
		newFormObj.Time = time.Now().Format("Jan 02 2024")
		
		// insert into mongo
		insertMsg, err := collection.InsertOne(context.TODO(), newFormObj)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": "Cannot Insert Msg Into Database"})
			return
		}
		fmt.Println(insertMsg)

		// return json data
		c.JSON(http.StatusOK, gin.H{"data": "Successfully Submitted Form"})
	}
}

func GetContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Println(time.Now())

		// invoke mongo db
		var result bson.M
		err := collection.FindOne(context.TODO(), bson.D{primitive.E{ Key: "name", Value: "Christopher Liu" }}).Decode(&result)
		if err == mongo.ErrNoDocuments {
			fmt.Println("No document found")
			return
		}
		if err != nil {
			fmt.Println(err)
		}

		c.JSON(200, result) 
    }
}