package invoices

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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

		fmt.Printf("%+v\n", *body.CurrPage)
		fmt.Printf("%+v\n", body.Filter)
		fmt.Printf("%+v\n", *body.Filter.Shipping)

		// construct shipping filter
		shippingFilter := bson.D{{}}
		isShipping := *body.Filter.Shipping
		if isShipping != "" {
			shippingFilter[0].Key = "isShipping"
			if isShipping == "pickup" {
				shippingFilter[0].Value = false
			} else if isShipping == "shipping" {
				shippingFilter[0].Value = true
			}
		}

		// destruct filters
		fil := bson.D{
			{
				Key: "$and",
				Value: bson.A{
					// bson.D{{Key: "currPage", Value: *body.CurrPage}},
					shippingFilter,
				},
			},
		}

		// new query options setting sort and skip
		opt := options.Find().SetSort(bson.D{{
			Key:   "timeCreated",
			Value: -1,
		}}).SetSkip(int64(*body.CurrPage))

		// find items in database using above options
		cursor, err := collection.Find(ctx, fil, opt)
		if err != nil {
			fmt.Println(err)
			c.String(http.StatusInternalServerError, "Cannot Get From Database")
			return
		}
		defer cursor.Close(ctx) // close it after query

		// store all result in bson array to return it
		var results []bson.M
		for cursor.Next(ctx) {
			var result bson.M
			err := cursor.Decode(&result)
			if err != nil {
				c.String(http.StatusInternalServerError, "Database Error!")
			}
			results = append(results, result)
		}

		fmt.Println(results)
		c.JSON(200, results)
	}
}
