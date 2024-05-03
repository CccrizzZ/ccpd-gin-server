package contact

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var timeFormat string = "2006-01-02 15:04:05"

type ContactUsForm struct {
	FirstName string `json:"firstname" binding:"required" validate:"required"`
	LastName  string `json:"lastname" binding:"required" validate:"required"`
	Phone     string `json:"phone" binding:"required" validate:"required"`
	Email     string `json:"email" binding:"required" validate:"required"`
	Invoice   string `json:"invoice" binding:"required" validate:"required"`
	Lot       string `json:"lot" binding:"required" validate:"required"`
	Reason    string `json:"reason" binding:"required" validate:"required"`
	Message   string `json:"message" binding:"required" validate:"required"`
	Time      string `json:"time"`
	IP        string `json:"ip"`
	Replied   bool   `json:"replied"`
}

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// validator v10
var validate = validator.New()

func SubmitContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind incoming json to struct
		var newFormObj ContactUsForm
		bindErr := c.ShouldBindJSON(&newFormObj)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Please Check Your Form Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(newFormObj); err != nil {
			c.String(http.StatusBadRequest, "Validation error: "+err.Error())
			return
		}

		// create time zone
		currTimeZone, err := time.LoadLocation("America/New_York")
		if err != nil {
			c.String(http.StatusInternalServerError, "Cannot Get EST")
			return
		}

		// check for IP repeating in 24h
		now := time.Now().In(currTimeZone)
		past24Hours := now.Add(-24 * time.Hour)
		filter := bson.M{
			"ip": c.ClientIP(),
			"time": bson.M{
				"$gte": past24Hours.Format(timeFormat),
				"$lt":  now.Format(timeFormat),
			},
		}

		// if message count greater than 2, return error
		count, err := collection.CountDocuments(ctx, filter)
		if err != nil {
			c.String(http.StatusInternalServerError, "Cannot Count Documents")
			return
		}
		if count > 2 {
			c.String(http.StatusBadRequest, "Cannot Send Messages, Daily Limits Reached!, Please Contact Us Through Email!")
			return
		}

		// fill form
		newFormObj.Time = now.In(currTimeZone).Format(timeFormat)
		newFormObj.IP = c.ClientIP()
		newFormObj.Replied = false

		// insert into mongo
		insertMsg, err := collection.InsertOne(ctx, newFormObj)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": "Cannot Insert Msg Into Database"})
			return
		}
		fmt.Println(insertMsg)

		// return json data
		c.JSON(http.StatusOK, gin.H{"data": "Successfully Submitted Form"})
	}
}

type ByPageRequest struct {
	CurrPage     *int `json:"currPage" binding:"required" validate:"required"`
	ItemsPerPage *int `json:"itemsPerPage" binding:"required" validate:"required"`
}

type PaginationResponse struct {
	Data       []primitive.M `json:"data"`
	TotalItems int64         `json:"totalItems"`
}

func GetContactFormByPage(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind body to JSON
		var body ByPageRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation error: " + err.Error()})
			return
		}

		// construct filter
		currPage := *body.CurrPage
		itemsPerPage := *body.ItemsPerPage
		fil := options.Find()
		skip := currPage * itemsPerPage
		fil.SetSkip(int64(skip))
		fil.SetLimit(int64(itemsPerPage))
		fil.SetSort(bson.D{{Key: "time", Value: -1}})

		// invoke mongo db
		cursor, err := collection.Find(ctx, bson.M{}, fil)
		if err == mongo.ErrNoDocuments {
			c.String(http.StatusBadRequest, "No Documents Found!")
			return
		}
		if err != nil {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		defer cursor.Close(ctx)

		// get all results
		var results []bson.M
		for cursor.Next(ctx) {
			var result bson.M
			err := cursor.Decode(&result)
			if err != nil {
				c.String(http.StatusInternalServerError, "Database Error!")
			}
			results = append(results, result)
		}

		// Get total documents
		total, err := collection.CountDocuments(ctx, options.FindOptions{})
		if err != nil {
			c.String(http.StatusBadRequest, "No Documents Found!")
			return
		}

		response := PaginationResponse{
			Data:       results,
			TotalItems: total,
		}

		c.JSON(200, response)
	}
}

type setRepliedRequest struct {
	Email   string `json:"email" binding:"required" validate:"required"`
	Time    string `json:"time" binding:"required" validate:"required"`
	Replied bool   `json:"replied" binding:"required" validate:"required"`
}

func SetContactFormReplied(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind body to JSON
		var body setRepliedRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"data": "Validation error: " + err.Error()})
			return
		}

		// update to mongo db
		insertMsg, err := collection.UpdateOne(
			ctx,
			bson.M{"email": body.Email, "time": body.Time},
			bson.M{"$set": bson.M{"replied": true}},
		)
		if err != nil {
			fmt.Println(err.Error())

			c.JSON(http.StatusInternalServerError, gin.H{"data": "Cannot Update Database!"})
			return
		}
		fmt.Println(insertMsg)

		c.String(http.StatusOK, "Message Status Updated!")
	}
}
