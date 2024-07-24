package invoices

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dslipak/pdf"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var timeFormat string = "2006-01-02T15:04:05Z07:00"

type Invoice struct {
	InvoiceNumber string  `json:"invoiceNumber" binding:"required" validate:"required"`
	Time          string  `json:"time" binding:"required" validate:"required"`
	BuyerName     string  `json:"buyerName" binding:"required" validate:"required"`
	BuyerEmail    string  `json:"buyerEmail" binding:"required" validate:"required"`
	AuctionLot    int     `json:"auctionLot" binding:"required" validate:"required"`
	InvoiceTotal  float32 `json:"invoiceTotal" binding:"required"`
	Message       string  `json:"message" binding:"required" validate:"required"`
}

type InvoiceItem struct {
	Msrp          string `json:"msrp"`
	ShelfLocation string `json:"shelfLocation"`
	Sku           int    `json:"sku"`
	ItemLot       int    `json:"itemLot"`
	Desc          string `json:"description"`
	Unit          int    `json:"unit"`
	UnitPrice     int    `json:"unitPrice"`
	ExtendedPrice int    `json:"extendedPrice"` // unit * unitPrice
	HandlingFee   int    `json:"handlingFee"`
}

func CreateInvoice() gin.HandlerFunc {
	return func(c *gin.Context) {
		// ctx := context.Background()
		var newInvoice Invoice
		bindErr := c.ShouldBindJSON(&newInvoice)
		if bindErr != nil {
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}
	}
}

type PDFRequest struct {
	PDF []byte `json:"pdf"`
}

func CreateInvoiceFromPDF() gin.HandlerFunc {
	return func(c *gin.Context) {
		// ctx := context.Background()
		// get files from form
		// form, err := c.MultipartForm()
		// if err != nil {
		// 	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		// 	return
		// }

		// open file from request
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.String(http.StatusBadRequest, "Cannot Read File")
			return
		}
		defer file.Close()

		// Check the file extension
		if filepath.Ext(header.Filename) != ".pdf" {
			c.String(http.StatusBadRequest, "Please Only Upload PDF File")
			return
		}

		// check file size
		if header.Size > 10*1024*1024 {
			c.String(http.StatusBadRequest, "File Size Must Not Exceed 10 MB")
			return
		}

		// create temp file from buffer
		tmp, err := os.CreateTemp("./", "temp_*.pdf")
		if err != nil {
			fmt.Println(err.Error())
			c.String(http.StatusInternalServerError, "Error Creating Temp File: %v", err)
			return
		}
		fmt.Println(tmp.Name())
		defer os.Remove(tmp.Name())
		defer tmp.Close()

		// write data into tmp file
		_, err = io.Copy(tmp, file)
		if err != nil {
			c.String(http.StatusInternalServerError, "Error Writing Temp File: %v", err)
			return
		}

		// multiple pdf
		// // Loop through all files in data form
		// for name, files := range form.File {
		// 	// open every file and upload
		// 	for _, fileHeader := range files {
		// 		if fileHeader.Size > 10*1024*1024 {
		// 			c.String(http.StatusBadRequest, "File Size Must Not Exceed 10 MB")
		// 			break
		// 		}
		// 	}
		// 	fmt.Println(name)
		// }

		// load with dslipakr pfd library
		// f, openErr := pdf.Open("./pkg/invoices/Invoice_25632.pdf")
		f, openErr := pdf.Open(tmp.Name())
		if openErr != nil {
			fmt.Println(openErr.Error())
			c.String(http.StatusInternalServerError, openErr.Error())
			return
		}

		// read plain text into buffer
		var buf bytes.Buffer
		reader, textErr := f.GetPlainText()
		if textErr != nil {
			fmt.Println(textErr.Error())
			c.String(http.StatusInternalServerError, textErr.Error())
			return
		}
		buf.ReadFrom(reader)

		// fmt.Println(buf.String())

		// err = os.Remove(tmp.Name())
		// if err != nil {
		// 	fmt.Println("Error deleting file:", err)
		// 	return
		// }

		// obj, err := parseInvoice(buf.String())
		// if err != nil {
		// 	fmt.Println(err)
		// }
		// fmt.Println(obj)

		// returns ok if success
		c.JSON(http.StatusOK, gin.H{
			"data": buf.String(),
			"size": header.Size,
		})
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

		// return the item info as json
		c.JSON(200, gin.H{
			"itemsArr":   itemsArr,
			"totalItems": totalItemsFilterd,
		})
	}
}

// datas for invoice controller charts
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
