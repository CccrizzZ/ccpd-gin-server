package invoices

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dslipak/pdf"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// var timeFormat string = "2006-01-02T15:04:05Z07:00"
var invoiceTimeFormat string = "2006-01-02 15:04:05 -0700 MST"

// bucket names
var signatureBucket string = "258-signatures"
var invoiceBucket string = "258-invoices"

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

type Invoice struct {
	InvoiceNumber    string         `json:"invoiceNumber" bson:"invoiceNumber" binding:"required" validate:"required"`
	Time             string         `json:"time" bson:"time" validate:"required"`
	BuyerName        string         `json:"buyerName" bson:"buyerName" validate:"required"`
	BuyerEmail       string         `json:"buyerEmail" bson:"buyerEmail" validate:"required"`
	BuyerAddress     string         `json:"buyerAddress" bson:"buyerAddress"`
	ShippingAddress  string         `json:"shippingAddress" bson:"shippingAddress"`
	BuyerPhone       string         `json:"buyerPhone" bson:"buyerPhone"`
	AuctionLot       int            `json:"auctionLot" bson:"auctionLot"`
	InvoiceTotal     float32        `json:"invoiceTotal" bson:"invoiceTotal"`
	RemainingBalance float32        `json:"remainingBalance" bson:"remainingBalance"`
	Tax              float64        `json:"tax" bson:"tax"`
	Status           string         `json:"status" bson:"status"`
	TotalHandlingFee float32        `json:"totalHandlingFee" bson:"totalHandlingFee"`
	PaymentMethod    string         `json:"paymentMethod" bson:"paymentMethod"`
	InvoiceEvent     []InvoiceEvent `json:"invoiceEvent" bson:"invoiceEvent"`
	Items            []InvoiceItem  `json:"items" bson:"items"`
	IsShipping       bool           `json:"isShipping" bson:"isShipping"`
	BuyersPremium    float32        `json:"buyersPremium" bson:"buyersPremium"`
	SignatureCdn     string         `json:"signatureCdn" bson:"signatureCdn"`
	InvoiceCdn       string         `json:"invoiceCdn" bson:"invoiceCdn"`
}

type InvoiceEvent struct {
	Title string `json:"title" bson:"title"`
	Desc  string `json:"desc" bson:"desc"`
	Time  string `json:"time" bson:"time"`
}

type InvoiceItem struct {
	Sku           int     `json:"sku" bson:"sku"`
	Msrp          float32 `json:"msrp" bson:"msrp"`
	ShelfLocation string  `json:"shelfLocation" bson:"shelfLocation"`
	ItemLot       int     `json:"itemLot" bson:"itemLot"`
	Desc          string  `json:"desc" bson:"desc"`
	Bid           float32 `json:"bid" bson:"bid"`
	Unit          float32 `json:"unit" bson:"unit"`
	ExtendedPrice float32 `json:"extendedPrice" bson:"extendedPrice"` // unit * unitPrice
	HandlingFee   float32 `json:"handlingFee" bson:"handlingFee"`
}

func getCDN(bucket string, fileName string) string {
	cdn := fmt.Sprintf("https://%s.%s/%s", bucket, "nyc3.cdn.digitaloceanspaces.com", fileName)
	return cdn
}

// upload single invoice pdf to digital ocean space object storage
// defaultting the CDN link to public viewable
func UploadToSpace(
	ctx context.Context,
	storageClient *minio.Client,
	bucket string,
	file multipart.File,
	header *multipart.FileHeader,
) string {
	// upload pdf file to space object storage
	uploaded, uploadErr := storageClient.PutObject(
		ctx,
		bucket,
		header.Filename,
		file,
		header.Size,
		minio.PutObjectOptions{
			ContentType: header.Header.Get("Content-Type"),
			UserMetadata: map[string]string{
				"x-amz-acl": "public-read",
			},
		},
	)
	if uploadErr != nil {
		fmt.Println(uploadErr)
	}

	// construct CDN url
	cdnURL := getCDN(bucket, uploaded.Key)
	// fmt.Println(cdnURL)
	return cdnURL
}

type SoldItem struct {
	Sku           int     `json:"sku" bson:"sku"`
	Lot           int     `json:"clotNumber" bson:"clotNumber"`
	Lead          string  `json:"lead" bson:"lead"`
	ShelfLocation string  `json:"shelfLocation" bson:"shelfLocation"`
	Bid           float64 `json:"bidAmount" bson:"bidAmount"`
}

// fill in the blank for invoice item array
// the invoice here is a reference the passed in variable will be modified
func FillItemDataFromDB(invoice Invoice, collection *mongo.Collection) (Invoice, error) {
	ctx := context.Background()

	// loop all invoice items
	var newItemArr []InvoiceItem
	for _, item := range invoice.Items {
		// construct mongo db filter
		fil := bson.M{
			"lot":                  invoice.AuctionLot,
			"soldItems.clotNumber": item.ItemLot,
		}

		// find item in remaining record
		var res struct{ SoldItems []SoldItem }
		err := collection.FindOne(
			ctx,
			fil,
			options.FindOne().SetProjection(bson.M{"soldItems.$": 1}),
		).Decode(&res)
		if err != nil {
			return invoice, errors.New("cannot find invoice item in remaining record")
		}

		// unpack and set datas
		inv := res.SoldItems[0]
		item.Desc = inv.Lead
		item.Bid = float32(inv.Bid)
		item.ShelfLocation = inv.ShelfLocation
		item.Sku = inv.Sku
		newItemArr = append(newItemArr, item)
	}
	invoice.Items = newItemArr
	return invoice, nil
}

// split the invoice text into parts
// header, items rows, footer
func splitInvoice(text string) (map[string]any, error) {
	var invoiceInfo = map[string]any{}

	// this split the body and header
	re := regexp.MustCompile(`PRICEEXTENDEDPRICE`)

	// get index of `PRICEEXTENDEDPRICE`
	index := re.FindStringIndex(text)

	// if found, store first part and rest in array
	var rest string
	if index != nil {
		header := text[:index[1]]
		invoiceInfo["header"] = header
		rest = text[index[1]:]
	} else {
		return invoiceInfo, errors.New("cannot find PRICEEXTENDEDPRICE to split the header")
	}

	// split the items and footer
	itemRe := regexp.MustCompile(`MSRP:(.*?)Item handling`)
	matches := itemRe.FindAllStringSubmatch(rest, -1)

	// store all items in an array
	var invoiceItems []string
	for _, match := range matches {
		if len(match) > 1 {
			invoiceItems = append(invoiceItems, strings.TrimSpace(match[1]))
		}
	}
	invoiceInfo["items"] = invoiceItems

	// get handling fee for all items
	handlingFeePattern := regexp.MustCompile(`Item handling fee\s*-\s*(.*?)\s*T`)
	handlingFeeMatches := handlingFeePattern.FindAllStringSubmatch(rest, -1)
	var handlingFees []string
	for _, match := range handlingFeeMatches {
		if len(match) > 1 {
			handlingFees = append(handlingFees, strings.TrimSpace(match[1]))
		}
	}
	invoiceInfo["itemHandlingFees"] = handlingFees

	// get unit for all items
	unitPattern := regexp.MustCompile(`(\d+)\s*x\s*\d+\.\d{2}`)
	unitMatches := unitPattern.FindAllStringSubmatch(rest, -1)
	var unit []float32
	for _, match := range unitMatches {
		f, err := strconv.ParseFloat(strings.TrimSpace(match[1]), 32)
		if err != nil {
			fmt.Println(err.Error())
		}
		unit = append(unit, float32(f))
	}
	invoiceInfo["unitsArr"] = unit
	fmt.Println(unit)

	// get footer
	pattern := `\d+\.\d{2}\s*Total Extended Price:`
	footerRe := regexp.MustCompile(pattern)
	matchIndex := footerRe.FindStringIndex(rest)
	if matchIndex != nil {
		after := rest[matchIndex[0]:]
		invoiceInfo["footer"] = after
	}

	fmt.Println(text)
	return invoiceInfo, nil
}

func processSplitInvoice(result map[string]any) Invoice {
	var newInvoice Invoice

	//  header items footer
	header := result["header"].(string)
	items := result["items"].([]string)
	footer := result["footer"].(string)

	// get auction lot
	auctionLotPattern := regexp.MustCompile(`Auction Sale - (\d+)`)
	auctionLotMatch := auctionLotPattern.FindStringSubmatch(header)
	if len(auctionLotMatch) > 1 {
		newInvoice.AuctionLot, _ = strconv.Atoi(auctionLotMatch[1])
	}

	// get invoice number
	invoiceNumberPattern := regexp.MustCompile(`\s+1\s+(\d+)\s*Auction Sale`)
	invoiceNumberMatch := invoiceNumberPattern.FindStringSubmatch(header)
	if len(invoiceNumberMatch) > 1 {
		newInvoice.InvoiceNumber = strings.TrimSpace(invoiceNumberMatch[1])
	}

	// get buyer name, address, shipping address, email
	// check if invoice is shipping
	isShipping := strings.Contains(header, "SHIP TO:")
	if isShipping {
		// set invoice status
		newInvoice.IsShipping = true

		// email will be in between "ship to:" and "lot #"
		buyerEmailPattern := regexp.MustCompile(`SHIP TO:\s*(.*?)Lot#`)
		buyerEmailMatch := buyerEmailPattern.FindStringSubmatch(header)
		if len(buyerEmailMatch) > 1 {
			newInvoice.BuyerEmail = strings.TrimSpace(buyerEmailMatch[1])
		}

		// buyer name and shipping address
		buyerNameAddressPattern := regexp.MustCompile(`SOLD TO:\s*(.*?)SHIP TO:`)
		buyerNameAddressPatternMatch := buyerNameAddressPattern.FindStringSubmatch(header)
		if len(buyerNameAddressPatternMatch) > 1 {
			buyerInfo := strings.TrimSpace(buyerNameAddressPatternMatch[1])

			// split buyer name and shipping address
			re := regexp.MustCompile(`\d`)
			firstNumberIndex := re.FindStringIndex(buyerInfo)
			if firstNumberIndex != nil {
				newInvoice.BuyerName = strings.TrimSpace(buyerInfo[:firstNumberIndex[0]])
				newInvoice.ShippingAddress = strings.TrimSpace(buyerInfo[firstNumberIndex[0]:])
			}
		}
	} else {
		// email will be in between "sold to:" and "lot #"
		buyerEmailPattern := regexp.MustCompile(`SOLD TO:\s*(.*?)Lot#`)
		buyerEmailMatch := buyerEmailPattern.FindStringSubmatch(header)
		if len(buyerEmailMatch) > 1 {
			newInvoice.BuyerEmail = strings.TrimSpace(buyerEmailMatch[1])
		}
	}

	// get invoice total and remaining balance
	invoiceBalancePattern := regexp.MustCompile(`Default:\s*(.*?)\s*Invoice Total:`)
	invoiceBalanceMatch := invoiceBalancePattern.FindStringSubmatch(footer)
	// flag to check if match is float
	var isFloat bool = true
	if len(invoiceBalanceMatch) > 1 {
		res := strings.TrimSpace(invoiceBalanceMatch[1])
		// repace $ with space
		clean := strings.ReplaceAll(res, "$", " ")

		// split into array
		parts := strings.Fields(clean)

		// parse float
		total, totalErr := strconv.ParseFloat(parts[0], 32)
		if totalErr != nil {
			isFloat = false
			fmt.Println(totalErr)
		}
		remaining, remainingErr := strconv.ParseFloat(parts[1], 32)
		if remainingErr != nil {
			isFloat = false
			fmt.Println(remainingErr)
		}

		// if they are float set them to invoice
		if isFloat {
			newInvoice.InvoiceTotal = float32(total)
			newInvoice.RemainingBalance = float32(remaining)
		}
	}

	if len(invoiceBalanceMatch) < 1 || !isFloat {
		invoiceBalancePattern2 := regexp.MustCompile(`PAID IN FULL\s*(.*?)\s*Invoice Total:`)
		invoiceBalanceMatch2 := invoiceBalancePattern2.FindStringSubmatch(footer)
		match := strings.TrimSpace(invoiceBalanceMatch2[1])
		// repace $ with space
		clean := strings.ReplaceAll(match, "$", " ")

		// split into array
		parts := strings.Fields(clean)

		// parse float
		total, totalErr := strconv.ParseFloat(parts[0], 32)
		if totalErr != nil {
			fmt.Println(totalErr)
		}
		remaining, remainingErr := strconv.ParseFloat(parts[1], 32)
		if remainingErr != nil {
			fmt.Println(remainingErr)
		}
		newInvoice.InvoiceTotal = float32(total)
		newInvoice.RemainingBalance = float32(remaining)
	}

	// get invoice time
	timePattern1 := regexp.MustCompile(`\)\s*(.*?)\s*Invoice #:`)
	timePattern2 := regexp.MustCompile(`(\d{1,2}/\d{1,2}/\d{4} \d{1,2}:\d{2}:\d{2})`)
	timeMatch1 := timePattern1.FindStringSubmatch(header)
	timeMatch2 := timePattern2.FindStringSubmatch(header)
	var timeStr string = ""
	if len(timeMatch1) > 1 {
		timeStr = strings.TrimSpace(timeMatch1[1])
	} else if len(timeMatch2) > 1 {
		timeStr = strings.TrimSpace(timeMatch2[1])
	}
	if timeStr != "" {
		var parsedTime, err = time.ParseInLocation(
			"2006-01-02 15:04:05",
			timeStr,
			time.Now().Location(),
		)
		if err != nil {
			parsedTime, err = time.ParseInLocation(
				"1/2/2006 3:04:05",
				timeStr,
				time.Now().Location(),
			)
			if err != nil {
				fmt.Println("cannot parse time")
			}
		}
		newInvoice.Time = parsedTime.String()
	}

	// get buyer address and name
	// if paid invoice the buyer address regex is different
	invoicePaid := strings.Contains(header, "PAID IN FULL")
	var buyerAddressPattern *regexp.Regexp
	if !invoicePaid {
		newInvoice.Status = "unpaid"
		buyerAddressPattern = regexp.MustCompile(`\*\*\d{4}(.*?)Phone`)
		newInvoice.InvoiceEvent = append(
			newInvoice.InvoiceEvent,
			InvoiceEvent{
				Title: "Invoice Unpaid",
				Desc:  "Invoice unpaid on issue",
				Time:  newInvoice.Time,
			},
		)
	} else {
		newInvoice.Status = "paid"
		newInvoice.PaymentMethod = "card"
		newInvoice.InvoiceEvent = append(
			newInvoice.InvoiceEvent,
			InvoiceEvent{
				Title: "Invoice Paid",
				Desc:  "Invoice paid on issue",
				Time:  newInvoice.Time,
			},
		)
		buyerAddressPattern = regexp.MustCompile(`PAID IN FULL(.*?)Phone`)
	}

	buyerAddressPatternMatch := buyerAddressPattern.FindStringSubmatch(header)
	if len(buyerAddressPatternMatch) > 1 {
		nameAddress := strings.TrimSpace(buyerAddressPatternMatch[1])
		// split buyer name and shipping address
		re := regexp.MustCompile(`\d`)
		firstNumberIndex := re.FindStringIndex(nameAddress)
		if firstNumberIndex != nil {
			if newInvoice.BuyerName == "" {
				newInvoice.BuyerName = strings.TrimSpace(nameAddress[:firstNumberIndex[0]])
			}
			newInvoice.BuyerAddress = strings.TrimSpace(nameAddress[firstNumberIndex[0]:])
		}
	}

	// buyer phone
	buyerPhonePattern := regexp.MustCompile(`Phone:\s*(.*?)\s*#`)
	buyerPhoneMatch := buyerPhonePattern.FindStringSubmatch(header)
	var buyerPhone string
	if len(buyerPhoneMatch) > 1 {
		buyerPhone = strings.TrimSpace(buyerPhoneMatch[1])
		buyerPhone = strings.ReplaceAll(buyerPhone, "-", "")
		buyerPhone = strings.ReplaceAll(buyerPhone, " ", "")
		newInvoice.BuyerPhone = buyerPhone
	}

	unitsArr := result["unitsArr"].([]float32)
	var itemsArr []InvoiceItem
	for index, value := range items {
		var invoiceItem InvoiceItem

		// get rid of dollar sign and T
		item := strings.ReplaceAll(value, "$", "")
		item = strings.ReplaceAll(item, "T", " ")
		item = strings.TrimSpace(item)
		// split into string array by space
		datas := strings.Fields(item)

		// set unit amount by units array
		invoiceItem.Unit = float32(unitsArr[index])

		// if check for error case
		// example error case $ 10.98Y17 43430T651 => 10.98Y17 43430 651 (len<4)
		// example error case $ 27.53 G1043239T563 => 27.53 G1043239 563 (len<4)
		if len(datas) == 4 {
			f, err := strconv.ParseFloat(datas[0], 32)
			if err != nil {
				fmt.Println(err)
			}
			invoiceItem.Msrp = float32(f)
			invoiceItem.ShelfLocation = datas[1]
			sku, convertErr := strconv.Atoi(datas[2])
			if convertErr == nil {
				invoiceItem.Sku = sku
			} else {
				fmt.Println(convertErr.Error())
			}
			itemLot, convertErr2 := strconv.Atoi(datas[3])
			if convertErr2 == nil {
				invoiceItem.ItemLot = itemLot
			} else {
				fmt.Println(convertErr.Error())
			}
		} else {
			// find the msrp by regex
			msrpPattern := regexp.MustCompile(`\$(.*?)[A-Za-z]`)
			msrpMatch := msrpPattern.FindStringSubmatch(value)
			if len(msrpMatch) > 1 {
				trimmed := strings.TrimSpace(msrpMatch[1])
				msrp, convertErr := strconv.ParseFloat(trimmed, 32)
				if convertErr != nil {
					fmt.Println(convertErr.Error())
				}
				invoiceItem.Msrp = float32(msrp)
			}

			// split it by T and take the lot number
			itemLot, convertErr := strconv.Atoi(datas[len(datas)-1])
			if convertErr == nil {
				invoiceItem.ItemLot = itemLot
			} else {
				fmt.Println(convertErr.Error())
			}
		}
		itemsArr = append(itemsArr, invoiceItem)
	}

	// get handling fees for each item and calculate total
	var totalHandlingFee float32
	itemsHandlingFees := result["itemHandlingFees"].([]string)
	for index, val := range itemsHandlingFees {
		f, err := strconv.ParseFloat(val, 32)
		if err != nil {
			fmt.Println(err.Error())
		}
		itemsArr[index].HandlingFee = float32(f)
		totalHandlingFee += float32(f)
	}

	// set total handling fee
	newInvoice.TotalHandlingFee = totalHandlingFee

	// set items array
	newInvoice.Items = itemsArr

	// get tax
	totalTaxPattern := regexp.MustCompile(`Quantity:\s*(.*?)\s*Tax1`)
	totalTaxMatch := totalTaxPattern.FindStringSubmatch(footer)
	if len(totalTaxMatch) > 1 {
		float, parseErr := strconv.ParseFloat(totalTaxMatch[1], 32)
		if parseErr != nil {
			fmt.Println(parseErr.Error())
		}
		newInvoice.Tax = float
	}

	// get buyer premium if there is any
	isBuyerPremium := strings.Contains(footer, "Premium:")
	if isBuyerPremium {
		buyersPremiumPattern := regexp.MustCompile(`(.*)Total Extended Price:`)
		buyersPremiumMatch := buyersPremiumPattern.FindStringSubmatch(footer)
		if len(buyersPremiumMatch) > 1 {
			float, parseErr := strconv.ParseFloat(buyersPremiumMatch[1], 64)
			if parseErr != nil {
				fmt.Println(parseErr.Error())
			}
			fl := roundFloat(float, 2)
			newInvoice.BuyersPremium = float32(fl)
		}
	} else {
		newInvoice.BuyersPremium = 0
	}

	return newInvoice
}

// this one only process UNPAID invoice pdf
func CreateInvoiceFromPDF(storageClient *minio.Client, collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// get files from form
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// parse upload pdf option from form value
		uploadPDF := c.Request.FormValue("uploadPDF")
		toUpload, err := strconv.ParseBool(uploadPDF)
		if err != nil {
			c.String(http.StatusBadRequest, "No Upload PDF Option Passed")
			return
		}

		// remove header and footer
		textToRemoveArr := []string{
			"Monday: CloseTuesday - Saturday: 12:00pm - 6:30pm",
			"CC Power Deals240 Bartor Road, Unit 4, North York, ON, M9M 2W6+1 416-740-2333",
			"READ NEW TERMS OF USE BEFORE YOU BID!",
			"READ EMAIL FOR PICK-UP & SHIPPING INSTRUCTIONS",
			"Sunday: CloseWe Asked All Items Should Check at Our Location",
			"NO RETURN AND REFUND",
			"#:Date:Page:UNPAIDLot#DESCRIPTIONUNIT PRICEEXTENDEDPRICE",
			"Monday & Sunday: CloseTuesday - Saturday: 12:00pm - 6:30pmWe Asked All Items Should Check at Our Location",
		}

		var invoices []Invoice
		// multiple pdf
		for name, files := range form.File {
			// open every file and upload
			for _, fileHeader := range files {
				// chekc file size
				if fileHeader.Size > 10*1024*1024 {
					c.String(http.StatusBadRequest, "File Size Must Not Exceed 10 MB")
					break
				}

				// open file from file handler
				file, err := fileHeader.Open()
				if err != nil {
					c.String(http.StatusBadRequest, "Cannot Read File")
					return
				}
				defer file.Close()

				// Check the file extension
				if filepath.Ext(name) != ".pdf" {
					c.String(http.StatusBadRequest, "Please Only Upload PDF File")
					return
				}

				var cdnLink string = ""
				// upload invoice to space object storage if uploadPDF in form is true
				if toUpload {
					// check if bucket exist
					exists, existErr := storageClient.BucketExists(ctx, invoiceBucket)
					if existErr != nil || !exists {
						c.String(http.StatusInternalServerError, "No Such Bucket")
						return
					}
					cdnLink = UploadToSpace(ctx, storageClient, invoiceBucket, file, fileHeader)
				}

				// create temp file from buffer
				tmp, createErr := os.CreateTemp("./pdf", "*.pdf")
				if createErr != nil {
					c.String(http.StatusInternalServerError, "Error Creating Temp File: %v", createErr.Error())
					return
				}
				defer func() {
					closeErr := tmp.Close()
					if closeErr != nil {
						fmt.Println(closeErr.Error())
					}
					removeErr := os.Remove(tmp.Name())
					if removeErr != nil {
						fmt.Println(removeErr.Error())
					}
				}()

				// write data into tmp file
				var _, copyErr = io.Copy(tmp, file)
				if copyErr != nil {
					c.String(http.StatusInternalServerError, "Error Writing Temp File: %v", copyErr.Error())
					return
				}

				// get file size
				tmpFileInformation, fileInfoErr := tmp.Stat()
				if fileInfoErr != nil {
					tmp.Close()
					c.String(http.StatusInternalServerError, fileInfoErr.Error())
				}

				// make reader
				pdfObj, readerErr := pdf.NewReader(tmp, tmpFileInformation.Size())
				if readerErr != nil {
					tmp.Close()
					c.String(http.StatusInternalServerError, readerErr.Error())
				}

				// read plain text into buffer
				var buf bytes.Buffer
				reader, textErr := pdfObj.GetPlainText()
				if textErr != nil {
					fmt.Println(textErr.Error())
					c.String(http.StatusInternalServerError, textErr.Error())
					return
				}

				// remove unwanted text
				buf.ReadFrom(reader)
				extractedText := buf.String()
				for _, val := range textToRemoveArr {
					extractedText = strings.ReplaceAll(extractedText, val, "")
				}

				// splite invoice text into 3 parts (header, items, footer)
				re, splitErr := splitInvoice(extractedText)
				if splitErr != nil {
					fmt.Println(splitErr.Error())
					c.String(http.StatusInternalServerError, splitErr.Error())
					return
				}

				// split and extract data with regex
				invoice := processSplitInvoice(re)
				// fill the inventories with data from database
				fixedInvoice, err1 := FillItemDataFromDB(invoice, collection)
				if err1 != nil {
					fmt.Println(err1)
				}
				fixedInvoice.InvoiceCdn = cdnLink
				invoices = append(invoices, fixedInvoice)
			}
		}

		// return invoice object
		c.JSON(http.StatusOK, gin.H{
			"data": invoices,
		})
	}
}

// update invoice data to database
func UpdateInvoice(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// bind json from request
		var newInvoice Invoice
		bindErr := c.ShouldBindJSON(&newInvoice)
		if bindErr != nil {
			fmt.Println(bindErr)
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		fmt.Println(newInvoice)

		// in, err := strconv.ParseInt(newInvoice.InvoiceNumber, 0, 32)
		// if err != nil {
		// 	c.String(400, "Cannot Convert Invoice Number to Int")
		// }

		// invNum, err := strconv.Atoi(newInvoice.InvoiceNumber)
		// if err != nil {
		// 	c.String(http.StatusBadRequest, "Invalid Body, %s", err.Error())
		// 	return
		// }

		// find and update
		res := collection.FindOneAndUpdate(
			ctx,
			bson.M{
				"auctionLot": newInvoice.AuctionLot,
				// "buyerName":  newInvoice.BuyerName,
				// "time":       newInvoice.Time,
				"invoiceNumber": newInvoice.InvoiceNumber,
			},
			bson.M{"$set": newInvoice},
			options.FindOneAndUpdate().SetReturnDocument(options.After),
		)
		var result bson.M
		decodeErr := res.Decode(&result)
		if result != nil && decodeErr != nil {
			c.String(200, "Update Success")
		}
	}
}

// push invoice data to database
func CreateInvoice(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// bind json
		var newInvoice []Invoice
		bindErr := c.ShouldBindJSON(&newInvoice)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		// loop all invoices
		for _, invoice := range newInvoice {
			count, err := collection.CountDocuments(
				ctx,
				bson.M{
					"buyerName":     invoice.BuyerName,
					"invoiceNumber": invoice.InvoiceNumber,
				},
			)
			if err != nil {
				c.String(500, "Cannot Count Documents")
				return
			}

			// if not found, insert it into db
			if count == 0 {
				_, err := collection.InsertOne(ctx, invoice)
				if err != nil {
					c.String(500, "Cannot Insert Documents")
					return
				}
			} else {
				c.String(500, "Documents Exists")
				return
			}
		}
		c.String(200, "Invoices Uploaded")
	}
}

type DeleteRequest struct {
	InvoiceNumber string `json:"invoiceNumber" bson:"invoiceNumber"`
	BuyerName     string `json:"buyerName" bson:"buyerName"`
	Time          string `json:"time" bson:"time"`
}

// delete invoice from database
func DeleteInvoice(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var request DeleteRequest
		bindErr := c.ShouldBindJSON(&request)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		res, err := collection.DeleteOne(
			ctx,
			bson.M{
				"invoiceNumber": request.InvoiceNumber,
				"buyerName":     request.BuyerName,
				"time":          request.Time,
			},
		)
		if err != nil {
			c.String(500, "Cannot Delete From Database")
			return
		}
		if res != nil {
			c.String(200, "Successfully Deleted")
		}
	}
}

var requestBody struct {
	Image  string `json:"image"`
	NewNum string `json:"newNum"`
}

func UploadSignature(storageClient *minio.Client, collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Decode JSON body
		if err := c.BindJSON(&requestBody); err != nil {
			fmt.Println(err.Error())
			c.JSON(400, gin.H{"error": "Invalid request payload"})
			return
		}

		// Decode Base64 image data
		imageData, err2 := base64.StdEncoding.DecodeString(requestBody.Image)
		if err2 != nil {
			fmt.Println(err2.Error())
			c.JSON(400, gin.H{"error": "Failed to decode Base64 image"})
			return
		}

		imageBuffer := bytes.NewBuffer(imageData)
		fmt.Println("BUF LEN:")
		fmt.Println(int64(imageBuffer.Len()))

		// get filename from path parameters
		invoiceNumber := c.Param("nom")

		// get files from form
		// form, err := c.MultipartForm()
		// if err != nil {
		// 	fmt.Println("Cannot Open File:", err)
		// 	// Cannot Open File: request Content-Type isn't multipart/form-data
		// 	c.String(http.StatusBadRequest, "Invalid Body")
		// 	return
		// }

		// sig := c.Request.FormValue("signature")
		// invoiceNumber := c.Request.FormValue("invoiceNumber")

		// fmt.Println(sig)
		// fmt.Println(invoiceNumber)

		// invoiceNumber := form.Value["invoiceNumber"]
		// files := form.File["signature"]
		// c.String(200, "OK")
		// return

		// loop through all files
		// var cdnLink string
		// for _, image := range files {
		// 	fh, err := image.Open()
		// 	if err != nil {
		// 		fmt.Println("Cannot Open File:", err)
		// 		c.String(http.StatusBadRequest, "Invalid Body")
		// 		fh.Close()
		// 		return
		// 	}
		// 	image.Filename = invoiceNumber[0] + "_sig"

		// 	// check if bucket exist in object storage
		// 	exists, existErr := storageClient.BucketExists(ctx, signatureBucket)
		// 	if existErr != nil || !exists {
		// 		c.String(http.StatusInternalServerError, "No Such Bucket")
		// 		return
		// 	}

		// 	// upload to digital ocean
		// 	cdnLink = UploadToSpace(ctx, storageClient, signatureBucket, fh, image)
		// }

		// put object into digital ocean space storage
		uploaded, uploadErr := storageClient.PutObject(
			ctx,
			signatureBucket,
			invoiceNumber+"_sig.png",
			imageBuffer,
			int64(imageBuffer.Len()),
			minio.PutObjectOptions{
				ContentType: "image/png",
				UserMetadata: map[string]string{
					"x-amz-acl": "public-read",
				},
			},
		)
		if uploadErr != nil {
			fmt.Println(uploadErr)
		}

		// construct CDN url
		cdnURL := fmt.Sprintf("https://%s.%s/%s", signatureBucket, "nyc3.digitaloceanspaces.com", uploaded.Key)

		// add signature event to invoice event
		now := time.Now()
		formattedTime := now.Format(invoiceTimeFormat)
		newEvent := InvoiceEvent{
			Title: "Pickup Signature",
			Desc:  "Customer signed and picked up",
			Time:  formattedTime,
		}

		// get the invoice number ({invoiceNumber}_{action})
		split := strings.Split(invoiceNumber, "_")
		fmt.Printf("Invoice: %s", split[0])

		// create new invoice
		if requestBody.NewNum != "" {
			// create new invoice
			var newInvoice Invoice = Invoice{
				InvoiceNumber: split[0],
				Status:        split[1],
				SignatureCdn:  cdnURL,
				InvoiceEvent:  []InvoiceEvent{newEvent},
			}

			// insert new document
			collection.InsertOne(ctx, newInvoice, nil)
		} else {
			// update information to existing invoice document
			collection.UpdateOne(
				ctx,
				bson.M{
					"invoiceNumber": split[0],
				},
				bson.M{
					"$push": bson.M{
						"invoiceEvent": newEvent,
					},
					"$set": bson.M{
						"status":       "pickedup",
						"signatureCdn": cdnURL,
					},
				},
			)
		}

		c.String(200, cdnURL)
	}
}

type DeleteSignatureReq struct {
	InvoiceNumber string `json:"invoiceNumber" bson:"invoiceNumber"`
	BuyerName     string `json:"buyerName" bson:"buyerName"`
	CDNLink       string `json:"cdnLink" bson:"cdnLink"`
}

func DeleteSignature(storageClient *minio.Client, collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var request DeleteSignatureReq
		bindErr := c.ShouldBindJSON(&request)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		// Parse the URL
		parsedURL, err := url.Parse(request.CDNLink)
		if err != nil {
			fmt.Println("Error parsing URL:", err)
			return
		}
		urlPath := parsedURL.Path
		fileName := path.Base(urlPath)

		// remove from object storage
		deleteErr := storageClient.RemoveObject(ctx, signatureBucket, fileName, minio.RemoveObjectOptions{})
		if deleteErr != nil {
			fmt.Println(deleteErr.Error())
			return
		}

		// delete cdn link from invoice
		collection.UpdateOne(
			ctx,
			bson.M{
				"invoiceNumber": request.InvoiceNumber,
				"buyerName":     request.BuyerName,
			},
			bson.M{
				"$set": bson.M{
					"signatureCdn": nil,
				},
			},
			nil,
		)

	}
}

// confirms the signature and deduct paid amount
func ConfirmSignature(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ctx := context.Background()

	}
}

type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type InvoiceFilter struct {
	// DateRange         []time.Time `json:"dateRange" binding:"required"`
	PaymentMethod     []string `json:"paymentMethod" binding:"required"`
	Status            []string `json:"status" binding:"required"`
	Shipping          *string  `json:"shipping" binding:"required"`
	FromDate          string   `json:"fromDate"`
	ToDate            string   `json:"toDate"`
	InvoiceTotalRange Range    `json:"invoiceTotalRange" binding:"required"`
	Keyword           *string  `json:"keyword" binding:"required"`
	InvoiceNumber     string   `json:"invoiceNumber"`
	AuctionLot        string   `json:"auctionLot"`
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

		// make date range filter
		timeFilter := bson.M{}
		if body.Filter.FromDate != "" {
			timeFilter["$gte"] = body.Filter.FromDate
		}
		if body.Filter.ToDate != "" {
			timeFilter["$lte"] = body.Filter.ToDate
		} else if body.Filter.FromDate != "" {
			// if only fromdate input and no todate
			// set todate to next day of fromdate
			dateObj, err := time.Parse(time.RFC3339, body.Filter.FromDate)
			if err != nil {
				fmt.Println("Error Parsing toDate:", err)
				return
			}
			newDate := dateObj.Add(24 * time.Hour)
			timeFilter["$lte"] = newDate.Format(time.RFC3339)
		}
		dateRangeFilter := bson.D{{
			Key:   "time",
			Value: timeFilter,
		}}

		// multiple payment method choices
		paymentMethodFilter := bson.D{{
			Key:   "$or",
			Value: nil,
		}}
		if len(body.Filter.PaymentMethod) > 0 {
			// loop all payment method, populate $or filter
			var tempArr = bson.A{}
			for _, val := range body.Filter.PaymentMethod {
				tempArr = append(tempArr, bson.M{"paymentMethod": val})
			}
			paymentMethodFilter[0].Value = tempArr
		}

		// invoice status filter
		statusFilter := bson.D{{
			Key:   "$or",
			Value: nil,
		}}
		if len(body.Filter.Status) > 0 {
			var tempArr = bson.A{}
			for _, val := range body.Filter.Status {
				tempArr = append(tempArr, bson.M{"status": val})
			}
			statusFilter[0].Value = tempArr
		}

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
		totalFilter := bson.M{}
		minInvoiceTotal := body.Filter.InvoiceTotalRange.Min
		if minInvoiceTotal != 0 {
			totalFilter["$gte"] = minInvoiceTotal
		}
		maxInvoiceTotal := body.Filter.InvoiceTotalRange.Max
		if maxInvoiceTotal != 999999 {
			totalFilter["$lte"] = maxInvoiceTotal
		}
		invoiceTotalFilter := bson.D{{
			Key:   "invoiceTotal",
			Value: totalFilter,
		}}

		// keyword filter
		kwFilter := bson.D{{
			Key:   "$or",
			Value: nil,
		}}
		words := strings.Fields(*body.Filter.Keyword)
		if len(words) > 0 {
			var tempArr = bson.A{}
			for _, val := range words {
				tempArr = append(tempArr, bson.M{"buyerAddress": bson.M{"$regex": val}})
				tempArr = append(tempArr, bson.M{"buyerEmail": bson.M{"$regex": val}})
				tempArr = append(tempArr, bson.M{"buyerName": bson.M{"$regex": val}})
			}
			kwFilter[0].Value = tempArr
		}

		// invoice number
		invoiceNumberFilter := bson.M{}
		// number, convertErr := strconv.Atoi(body.Filter.InvoiceNumber)
		if body.Filter.InvoiceNumber != "" {
			invoiceNumberFilter["invoiceNumber"] = body.Filter.InvoiceNumber
		}

		// auction lot
		auctionLotFilter := bson.M{}
		if body.Filter.AuctionLot != "" {
			intLot, err := strconv.ParseInt(body.Filter.AuctionLot, 0, 32)
			if err != nil {
				c.String(500, "Cannot Parse Auction Lot")
				return
			}
			auctionLotFilter["auctionLot"] = intLot
		}

		// make mongodb query filter
		andFilters := bson.A{
			shippingFilter,
			invoiceNumberFilter,
			auctionLotFilter,
		}
		// if payment method passed in, append payment method filter
		if paymentMethodFilter[0].Value != nil {
			andFilters = append(andFilters, paymentMethodFilter)
		}
		// same with invoice status
		if statusFilter[0].Value != nil {
			andFilters = append(andFilters, statusFilter)
		}
		if kwFilter[0].Value != nil {
			andFilters = append(andFilters, kwFilter)
		}
		// if one of the date range passed in, append the date filter
		if timeFilter["$gte"] != nil || timeFilter["$lte"] != nil {
			andFilters = append(andFilters, dateRangeFilter)
		}
		if totalFilter["$gte"] != nil || totalFilter["$lte"] != nil {
			andFilters = append(andFilters, invoiceTotalFilter)
		}
		fil := bson.D{
			{
				Key:   "$and",
				Value: andFilters,
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

		// count filtered items
		totalItemsFilterd, countErr := collection.CountDocuments(ctx, fil)
		if countErr != nil {
			c.String(http.StatusInternalServerError, "Cannot Get From Database")
			return
		}

		// store all result in array of objects
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

type InvoiceNumberRequest struct {
	InvoiceNumber string `json:"invoiceNumber"`
}

func GetInvoiceByInvoiceNumber(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var request InvoiceNumberRequest
		bindErr := c.ShouldBindJSON(&request)
		if bindErr != nil {
			fmt.Println(bindErr.Error())
			c.String(http.StatusBadRequest, "Invalid Body")
			return
		}

		// use find one to get target invoice
		opt := options.FindOne().SetProjection(
			bson.D{
				{Key: "_id", Value: 0},
			},
		)
		var inv Invoice
		res := collection.FindOne(
			ctx,
			bson.M{
				"invoiceNumber": request.InvoiceNumber,
			},
			opt,
		).Decode(&inv)

		if res != nil {
			c.JSON(200, inv)
		}
	}
}

type BarChartData struct {
	Month     string
	Cash      float32
	Card      float32
	Etransfer float32
}

type LineChartData struct {
	Lot    int32
	Amount float32
}

// convert numeric month to string
func numberToMonth(number int64) string {
	switch number {
	case 1:
		return "January"
	case 2:
		return "February"
	case 3:
		return "March"
	case 4:
		return "April"
	case 5:
		return "May"
	case 6:
		return "June"
	case 7:
		return "July"
	case 8:
		return "August"
	case 9:
		return "September"
	case 10:
		return "October"
	case 11:
		return "November"
	case 12:
		return "December"
	default:
		return ""
	}
}

// datas for invoice controller charts
func GetChartData(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		// format
		// const data = [
		// 	{ month: 'January', Cash: 1200, Card: 900, "E-transfer": 200 },
		// 	{ month: 'February', Cash: 1900, Card: 1200, "E-transfer": 400 },
		// 	{ month: 'March', Cash: 400, Card: 1000, "E-transfer": 200 },
		// 	{ month: 'April', Cash: 1000, Card: 200, "E-transfer": 800 },
		// 	{ month: 'May', Cash: 800, Card: 1400, "E-transfer": 1200 },
		// 	{ month: 'June', Cash: 750, Card: 600, "E-transfer": 1000 },
		//   ]

		// get 6 month before
		now := time.Now()
		start := now.AddDate(0, -6, 0)
		// start.Month()

		// find all document where payment method not null
		fil := bson.M{
			"time": bson.M{
				"$gte": start.Format(time.RFC3339),
			},
			"paymentMethod": bson.M{
				"$ne": nil,
			},
		}

		// chart datas
		// barChartData := []BarChartData{}
		// lineChartData := []LineChartData{}

		// find all
		curs, err := collection.Find(ctx, fil, nil)
		if err != nil {
			fmt.Print(err.Error())
			c.String(500, "Cannot Find Records")
			return
		}
		defer curs.Close(ctx)

		var cashData map[string]float32 = map[string]float32{}
		var cardData map[string]float32 = map[string]float32{}
		var etransferData map[string]float32 = map[string]float32{}

		// key: auction lot, val: auction total
		var auctionLotTotal map[int32]float32 = map[int32]float32{}

		// loop cursor
		for curs.Next(ctx) {
			var result Invoice
			err := curs.Decode(&result)
			if err != nil {
				fmt.Println(err)
				return
			}

			// determine invoice month
			t, err := time.Parse(invoiceTimeFormat, result.Time)
			if err != nil {
				fmt.Println("Error parsing date:", err)
				return
			}

			// convert month to string
			month := numberToMonth(int64(t.Month()))
			fmt.Println(month)

			// add invoice total to totals
			switch result.PaymentMethod {
			case "cash":
				cashData[month] += result.InvoiceTotal
			case "card":
				cardData[month] += result.InvoiceTotal
			case "etransfer":
				etransferData[month] += result.InvoiceTotal
			}

			auctionLotTotal[int32(result.AuctionLot)] += result.InvoiceTotal
		}

		var barData []BarChartData
		for month, total := range cashData {
			found := false
			for _, val := range barData {
				if val.Month == month {
					ref := &val
					ref.Cash += total
					found = true
				}
			}
			if !found {
				barData = append(barData, BarChartData{
					Cash:  total,
					Month: month,
				})
			}
		}

		for month, total := range cardData {
			found := false
			for _, val := range barData {
				if val.Month == month {
					ref := &val
					ref.Card += total
					found = true
				}
			}
			if !found {
				barData = append(barData, BarChartData{
					Card:  total,
					Month: month,
				})
			}
		}

		for month, total := range etransferData {
			found := false
			for _, val := range barData {
				if val.Month == month {
					ref := &val
					ref.Etransfer += total
					found = true
				}
			}
			if !found {
				barData = append(barData, BarChartData{
					Etransfer: total,
					Month:     month,
				})
			}
		}

		fmt.Println(auctionLotTotal)
		fmt.Println(barData)
		c.JSON(200, barData)
	}
}

func GetAllInvoiceLot(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		res, err := collection.Distinct(
			context.Background(),
			"auctionLot",
			bson.M{},
			nil,
		)
		if err != nil {
			fmt.Println(err.Error())
			c.String(500, "Cannot Get Distinct")
			return
		}
		c.JSON(200, res)
	}
}

type VerifyReq = struct {
	InvoiceNumber string `json:"invoiceNumber" bson:"invoiceNumber"`
}

type VerifyRes = struct {
	InvoiceNumber string `json:"invoiceNumber" bson:"invoiceNumber"`
	BuyerName     string `json:"buyerName" bson:"buyerName"`
}

func VerifyInvoiceNumber(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req VerifyReq
		err := c.ShouldBindJSON(&req)
		if err != nil {
			fmt.Println(err.Error())
			c.String(400, "Invalid Body")
			return
		}

		// pull from database
		var responseData VerifyRes
		if req.InvoiceNumber != "" {
			err := collection.FindOne(
				context.Background(),
				bson.M{"invoiceNumber": req.InvoiceNumber},
				options.FindOne().SetProjection(bson.M{"buyerName": 1, "invoiceNumber": 1}),
			).Decode(&responseData)
			if err != nil {
				c.String(500, "Cannot Decode Record")
				return
			}
		} else {
			c.String(400, "Invalid Body")
			return
		}

		// if no results return 404
		if responseData.BuyerName == "" || responseData.InvoiceNumber == "" {
			c.String(404, "Invoice Incomplete")
			return
		}

		fmt.Println(responseData)
		c.JSON(200, responseData)
	}
}

type RefundReq = struct {
	InvoiceNumber string        `json:"invoiceNumber" bson:"invoiceNumber"`
	RefundItems   []InvoiceItem `json:"refundItems" bson:"refundItems"`
}

// takes invoice number and refund item array, add refund invoice event, then
func RefundInvoice(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RefundReq
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.String(400, "Invalid Body")
		}
		fmt.Println(req)

		var refundSum float32 = 0
		for _, item := range req.RefundItems {
			refundSum += float32(item.Bid)
			refundSum += float32(item.HandlingFee)
		}

		fmt.Println(len(req.RefundItems))
		fmt.Println(refundSum)

		desc := fmt.Sprintf("Refund: %d Items, Total: %f", len(req.RefundItems), refundSum)

		// construct invoice event
		currentTime := time.Now()
		var newRefundEvent = InvoiceEvent{
			Title: "Refund",
			Desc:  desc,
			Time:  currentTime.Format(time.RFC3339),
		}
		fmt.Println(newRefundEvent)

		// create new field called refundArr on document set it to req.RefundItems
		// update refund info to database document
		collection.FindOneAndUpdate(
			context.Background(),
			bson.M{
				"invoiceNumber": req.InvoiceNumber,
			},
			bson.M{
				"$set": bson.M{
					"":       newRefundEvent,
					"status": "refund",
				},
				"$push": bson.M{"invoiceEvent": newRefundEvent},
			},
			options.FindOneAndUpdate().SetProjection(bson.M{
				"InvoiceNumber": 1,
				"InvoiceTotal":  1,
			}),
		)
	}
}

func SearchSignatureByInvoice(storageClient *minio.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		var req struct {
			InvoiceNumber string `json:"invoiceNumber"`
		}
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.String(400, "Invalid Body %s", err.Error())
			return
		}

		pickupSig := ""
		returnSig := ""
		bucketName := "258-signatures"
		for object := range storageClient.ListObjects(
			ctx,
			bucketName,
			minio.ListObjectsOptions{
				Recursive: true,
				Prefix:    req.InvoiceNumber,
			},
		) {
			if strings.Contains(object.Key, "return") {
				returnSig = getCDN(bucketName, object.Key)
			}
			if strings.Contains(object.Key, "pickup") {
				pickupSig = getCDN(bucketName, object.Key)
			}
		}
		c.JSON(200, gin.H{"pickup": pickupSig, "return": returnSig})
	}
}
