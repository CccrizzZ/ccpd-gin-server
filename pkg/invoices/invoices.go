package invoices

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var timeFormat string = "2006-01-02T15:04:05Z07:00"

type NewInvoice struct {
	InvoiceNumber string `json:"invoiceNumber" binding:"required" validate:"required"`
	Message       string `json:"message" binding:"required" validate:"required"`
	InvoiceTotal  string `json:"invoiceTotal" binding:"required"`
}

func CreateInvoice() gin.HandlerFunc {
	return func(c *gin.Context) {

	}
}

type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type InvoiceFilter struct {
	PaymentMethod     []*string   `json:"paymentMethod" binding:"required"`
	Status            []*string   `json:"status" binding:"required"`
	Shipping          *string     `json:"shipping" binding:"required"`
	DateRange         []time.Time `json:"dateRange" binding:"required"`
	InvoiceTotalRange Range       `json:"invoiceTotalRange" binding:"required"`
	Keyword           *string     `json:"keyword" binding:"required"`
}

type GetInvoiceRequest struct {
	CurrPage     *int           `json:"currPage" binding:"required"`
	ItemsPerPage *int           `json:"itemsPerPage" binding:"required"`
	Filter       *InvoiceFilter `json:"filter" binding:"required"`
	TimeOrder    *int           `json:"timeOrder" binding:"required"`
}

func GetInvoicesByPage(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.TODO()
		var body GetInvoiceRequest
		bindErr := c.ShouldBindJSON(&body)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		fmt.Printf("CurrPage: %+v\n", *body.CurrPage)
		fmt.Printf("ItemsPerPage: %+v\n", *body.Filter)
		fmt.Printf("isShipping: %+v\n", *body.Filter.Shipping)

		// construct shipping filter
		shippingFilter := bson.D{{}}
		isShipping := *body.Filter.Shipping
		// all selection will exclude shipping from request body
		if isShipping != "" {
			// set key
			shippingFilter[0].Key = "isShipping"
			// set filter value
			if isShipping == "pickup" {
				shippingFilter[0].Value = false
			} else if isShipping == "shipping" {
				shippingFilter[0].Value = true
			}
		}

		// construct payment method filter
		// var invoiceTotalRange bson.D
		// totalRange := body.Filter.InvoiceTotalRange
		// invoiceTotalRange[0].Key = totalRange.Min

		// make mongo db filters
		fil := bson.D{
			{
				Key: "$and",
				Value: bson.A{
					shippingFilter,
				},
			},
		}

		// new query options setting sort and skip
		itemsPerPage := int64(*body.ItemsPerPage)
		opt := options.Find().SetSort(bson.D{{
			Key:   "time",
			Value: int32(*body.TimeOrder),
		}}).SetSkip(int64(*body.CurrPage) * itemsPerPage).SetLimit(itemsPerPage)

		// find items in database using above options
		cursor, err := collection.Find(ctx, fil, opt)
		if err != nil {
			fmt.Println(err)
			c.String(http.StatusInternalServerError, "Cannot Get From Database")
			return
		}
		defer cursor.Close(ctx) // close it after query

		// count items
		totalItemsFilterd, countErr := collection.CountDocuments(ctx, fil)
		if countErr != nil {
			c.String(http.StatusInternalServerError, "Cannot Get From Database")
			return
		}

		// store all result in bson array to return it
		var itemsArr []bson.M
		for cursor.Next(ctx) {
			var result bson.M
			err := cursor.Decode(&result)
			if err != nil {
				c.String(http.StatusInternalServerError, "Database Error!")
			}
			itemsArr = append(itemsArr, result)
		}

		c.JSON(200, gin.H{
			"itemsArr":   itemsArr,
			"totalItems": totalItemsFilterd,
		})
	}
}

// date for invoice controller charts
func GetChartData(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {

	}
}

// convert all time strings to RFC3339 format
func ConvertAllTimes(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		cursor, err := collection.Find(ctx, bson.M{"time": bson.M{"$exists": true}}, nil)
		if err != nil {
			log.Fatal("cannot find")
		}
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var document bson.M
			err := cursor.Decode(&document)
			if err != nil {
				log.Fatal("cannot decode cursor")
			}
			parsedTime, err := time.Parse("1/2/2006, 3:04:05 PM", document["time"].(string))
			if err != nil {
				log.Fatal("cannot parse time")
			}
			newTime := parsedTime.Format(timeFormat)
			fmt.Println(newTime)
			updateOption := bson.M{
				"$set": bson.M{
					"time": newTime,
				},
			}
			_, updateErr := collection.UpdateOne(ctx, bson.M{"_id": document["_id"]}, updateOption)
			if updateErr != nil {
				log.Fatal("Cannot update")
			}
		}
		c.String(200, "Pass")
	}
}
