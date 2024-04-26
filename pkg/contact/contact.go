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
}

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// validator v10
var validate = validator.New()

func SubmitContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// bind incoming json to struct
		var newFormObj ContactUsForm
		bindErr := c.ShouldBindJSON(&newFormObj)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Please Check Your Form Inputs!")
			return
		}

		// validate with validtor
		if err := validate.Struct(newFormObj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation error: " + err.Error()})
			return
		}

		// generate and set EST time
		currTime, err := time.LoadLocation("America/New_York")
		if err != nil {
			c.String(http.StatusInternalServerError, "Cannot Get Eastern Standard Time")
			return
		}
		newFormObj.Time = time.Now().In(currTime).Format(time.UnixDate)
		fmt.Println(newFormObj)
		newFormObj.IP = c.ClientIP()

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
			c.String(http.StatusBadRequest, "Please Check Your Inputs!")
			return
		}

		// pull pagination data
		currPage := *body.CurrPage
		itemsPerPage := *body.ItemsPerPage
		limit := currPage * itemsPerPage

		// validate with validtor
		if err := validate.Struct(body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation error: " + err.Error()})
			return
		}

		// construct filter
		fil := options.Find()
		fil.SetSort(bson.D{{Key: "time", Value: -1}})
		skip := (currPage - 1) * limit
		fil.SetSkip(int64(skip))
		fil.SetLimit(int64(limit))

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
