package contact

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	Replied   string `json:"replied"`
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
		newFormObj.Replied = "No"

		// remove space
		newFormObj.Invoice = strings.ReplaceAll(newFormObj.Invoice, " ", "")
		newFormObj.Lot = strings.ReplaceAll(newFormObj.Lot, " ", "")
		newFormObj.LastName = strings.ReplaceAll(newFormObj.LastName, " ", "")

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
	CurrPage      *int   `json:"currPage" binding:"required" validate:"required"`
	ItemsPerPage  *int   `json:"itemsPerPage" binding:"required" validate:"required"`
	SearchKeyword string `json:"searchKeyword"`
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
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		// validate with validtor
		if err := validate.Struct(body); err != nil {
			c.String(http.StatusBadRequest, "Validation error: "+err.Error())
			return
		}

		// construct filter
		currPage := body.CurrPage
		itemsPerPage := body.ItemsPerPage
		opt := options.Find()
		skip := (*currPage) * (*itemsPerPage)
		opt.SetSkip(int64(skip))
		opt.SetLimit(int64(*itemsPerPage))
		opt.SetSort(bson.D{{Key: "time", Value: -1}})

		// construct keyword filter object
		var filter bson.M = bson.M{}
		if body.SearchKeyword != "" {
			var orObject = []bson.M{}

			// check if is intiger
			if intKeyword, err := strconv.Atoi(body.SearchKeyword); err == nil {
				// push invoice and lot
				regexObj := bson.M{
					"$regex":   strconv.Itoa(intKeyword),
					"$options": "i",
				}
				orObject = append(orObject, bson.M{"invoice": regexObj})
				orObject = append(orObject, bson.M{"lot": regexObj})
				orObject = append(orObject, bson.M{"phone": regexObj})
			} else {
				// push invoice and lot
				regexObj := bson.M{
					"$regex":   body.SearchKeyword,
					"$options": "i",
				}
				orObject = append(orObject, bson.M{"reason": regexObj})
				orObject = append(orObject, bson.M{"message": regexObj})
				orObject = append(orObject, bson.M{"firstname": regexObj})
				orObject = append(orObject, bson.M{"lastname": regexObj})
				orObject = append(orObject, bson.M{"email": regexObj})
				orObject = append(orObject, bson.M{"time": regexObj})
			}

			// add $or object into filter
			filter["$or"] = orObject
		}

		// invoke mongo db
		cursor, err := collection.Find(ctx, filter, opt)
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
	Email     string `json:"email" binding:"required" validate:"required"`
	Time      string `json:"time" binding:"required" validate:"required"`
	SalesName string `json:"salesName" binding:"required" validate:"required"`
}

func SetContactFormReplied(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		// bind body to JSON
		var body setRepliedRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"data": "Validation error: " + err.Error()})
			return
		}

		// create time zone
		currTimeZone, err := time.LoadLocation("America/New_York")
		if err != nil {
			c.String(http.StatusInternalServerError, "Cannot Get EST")
			return
		}

		// update to mongo db
		insertMsg, err := collection.UpdateOne(
			ctx,
			bson.M{"email": body.Email, "time": body.Time},
			bson.M{
				"$set": bson.M{
					"replied":   time.Now().In(currTimeZone).Format(timeFormat),
					"salesName": body.SalesName,
				},
			},
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"data": "Cannot Update Database!"})
			return
		}
		fmt.Println(insertMsg)

		c.String(http.StatusOK, "Message Status Updated!")
	}
}
