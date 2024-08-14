package invoices

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
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

var timeFormat string = "2006-01-02T15:04:05Z07:00"

type Invoice struct {
	InvoiceNumber    string         `json:"invoiceNumber" bson:"invoiceNumber" binding:"required" validate:"required"`
	Time             string         `json:"time" bson:"time" binding:"required" validate:"required"`
	BuyerName        string         `json:"buyerName" bson:"buyerName" binding:"required" validate:"required"`
	BuyerEmail       string         `json:"buyerEmail" bson:"buyerEmail" binding:"required" validate:"required"`
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
}

type InvoiceEvent struct {
	Desc  string `json:"desc" bson:"desc"`
	Title string `json:"title" bson:"title"`
	Time  string `json:"time" bson:"time"`
}

type InvoiceItem struct {
	Sku           int     `json:"sku" bson:"sku"`
	Msrp          float32 `json:"msrp" bson:"msrp"`
	ShelfLocation string  `json:"shelfLocation" bson:"shelfLocation"`
	ItemLot       int     `json:"itemLot" bson:"itemLot"`
	Desc          string  `json:"description" bson:"description"`
	Bid           float32 `json:"bid" bson:"bid"`
	ExtendedPrice float32 `json:"extendedPrice" bson:"extendedPrice"` // unit * unitPrice
	HandlingFee   float32 `json:"handlingFee" bson:"handlingFee"`
}

// upload single invoice pdf to digital ocean space object storage
// defaultting the CDN link to public viewable
func UploadInvoice(
	ctx context.Context,
	storageClient *minio.Client,
	file multipart.File,
	header *multipart.FileHeader,
) string {
	// upload pdf file to space object storage
	uploaded, uploadErr := storageClient.PutObject(
		ctx,
		"Invoices",
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
		log.Fatal(uploadErr)
	}

	// construct CDN url
	cdnURL := fmt.Sprintf("https://%s.%s/%s/%s", "crm-258-storage", "nyc3.cdn.digitaloceanspaces.com", uploaded.Bucket, uploaded.Key)
	fmt.Println(cdnURL)
	return cdnURL
}

// fill in the blank for invoice item array
// the invoice here is a reference the passed in variable will be modified
func FillItemDataFromDB(invoice *Invoice, client *mongo.Collection) error {
	ctx := context.Background()

	// loop all invoice items
	for _, item := range invoice.Items {
		fil := bson.M{
			"lot": invoice.AuctionLot,
			"soldItems": bson.M{
				"$elemMatch": bson.M{
					"clotNumber": item.ItemLot,
				},
			},
		}

		// find item in remaining record
		var res bson.M
		err := client.FindOne(ctx, fil, options.FindOne().SetProjection(bson.M{})).Decode(res)
		if err != nil {
			return errors.New("cannot find invoice item in remaining record")
		}

		// get lead
		if lead, ok := res["lead"].(string); ok {
			if item.Desc == "" {
				item.Desc = lead
			}
		} else {
			return errors.New("cannot get lead")
		}

		// get bid
		if bid, ok := res["bidAmount"].(float32); ok {
			if item.Bid == 0 {
				item.Bid = float32(bid)
			}
		} else {
			return errors.New("cannot get bid")
		}

		// shelf location
		if shelfLocation, ok := res["shelfLocation"].(string); ok {
			if item.ShelfLocation == "" {
				item.ShelfLocation = shelfLocation
			}
		} else {
			return errors.New("cannot get shelf location")
		}
	}

	return nil
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

	// get handling fee for all items
	handlingFeePattern := regexp.MustCompile(`Item handling fee\s*-\s*(.*?)\s*T`)
	handlingFeeMatches := handlingFeePattern.FindAllStringSubmatch(rest, -1)
	var handlingFees []string
	for _, match := range handlingFeeMatches {
		if len(match) > 1 {
			handlingFees = append(handlingFees, strings.TrimSpace(match[1]))
		}
	}

	// add invoice items array
	invoiceInfo["items"] = invoiceItems
	invoiceInfo["itemHandlingFees"] = handlingFees

	// get footer
	pattern := `\d+\.\d{2}\s*Total Extended Price:`
	footerRe := regexp.MustCompile(pattern)

	// get match index
	matchIndex := footerRe.FindStringIndex(rest)
	if matchIndex != nil {
		after := rest[matchIndex[0]:]
		invoiceInfo["footer"] = after
	}
	fmt.Println(invoiceInfo)
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
	if len(invoiceBalanceMatch) > 1 {
		res := strings.TrimSpace(invoiceBalanceMatch[1])
		clean := strings.ReplaceAll(res, "$", " ")
		parts := strings.Fields(clean)
		total, totalErr := strconv.ParseFloat(parts[0], 32)
		if totalErr != nil {
			fmt.Println(totalErr.Error())
		}
		remaining, remainingErr := strconv.ParseFloat(parts[1], 32)
		if remainingErr != nil {
			fmt.Println(remainingErr.Error())
		}
		newInvoice.InvoiceTotal = float32(total)
		newInvoice.RemainingBalance = float32(remaining)
	}

	// get invoice time
	timePattern := regexp.MustCompile(`\)\s*(.*?)\s*Invoice #:`)
	timeMatch := timePattern.FindStringSubmatch(header)
	if len(timeMatch) > 1 {
		// convert time into iso format
		var parsedTime, err = time.Parse("2006-01-02 15:04:05", strings.TrimSpace(timeMatch[1]))
		if err != nil {
			parsedTime, err = time.Parse("1/2/2006 3:04:05", strings.TrimSpace(timeMatch[1]))
			if err != nil {
				fmt.Println("cannot parse time")
			}
		}
		isoTime := parsedTime.Format(time.RFC3339)
		newInvoice.Time = isoTime
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
				Time:  strings.TrimSpace(timeMatch[1]),
			},
		)
	} else {
		newInvoice.Status = "paid"
		isCard := strings.Contains(header, "Auth#")
		if isCard {
			newInvoice.PaymentMethod = "card"
		}
		newInvoice.InvoiceEvent = append(
			newInvoice.InvoiceEvent,
			InvoiceEvent{
				Title: "Invoice Paid",
				Desc:  "Invoice paid on issue",
				Time:  strings.TrimSpace(timeMatch[1]),
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

	var itemsArr []InvoiceItem
	for _, value := range items {
		var invoiceItem InvoiceItem

		// get rid of dollar sign and T
		item := strings.ReplaceAll(value, "$", "")
		item = strings.ReplaceAll(item, "T", " ")
		item = strings.TrimSpace(item)
		// split into string array by space
		datas := strings.Fields(item)

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

	return newInvoice
}

// generated by chat gpt
// parse the extracted text from pdf to object
func parseInvoice(text string) Invoice {
	// auction lot number
	auctionLotPattern := regexp.MustCompile(`Auction Sale - (\d+)`)
	auctionLotMatch := auctionLotPattern.FindStringSubmatch(text)
	var auctionLot int
	if len(auctionLotMatch) > 1 {
		auctionLot, _ = strconv.Atoi(auctionLotMatch[1])
	}

	// Extract invoice number
	invoiceNumberPattern := regexp.MustCompile(`\s+1\s+(\d+)\s*Auction Sale`)
	invoiceNumberMatch := invoiceNumberPattern.FindStringSubmatch(text)
	var invoiceNumber string
	if len(invoiceNumberMatch) > 1 {
		invoiceNumber = strings.TrimSpace(invoiceNumberMatch[1])
	}

	// Extract time
	timePattern := regexp.MustCompile(`(\d{1,2}/\d{1,2}/\d{4}\s+\d{1,2}:\d{2}:\d{2})`)
	timeMatch := timePattern.FindStringSubmatch(text)
	var invoiceTime string
	if len(timeMatch) > 1 {
		invoiceTime = strings.TrimSpace(timeMatch[1])
	}

	// convert time into iso format
	parsedTime, err := time.Parse("1/2/2006 3:04:05", invoiceTime)
	if err != nil {
		log.Fatal("cannot parse time")
	}
	isoTime := parsedTime.Format(timeFormat)

	// buyer name & address
	buyerNamePattern := regexp.MustCompile(`SOLD TO:\s*(.*?)SHIP TO:`)
	buyerNameAddressMatch := buyerNamePattern.FindStringSubmatch(text)
	var buyerName string
	var buyerAddress string
	if len(buyerNameAddressMatch) > 1 {
		buyerInfo := strings.TrimSpace(buyerNameAddressMatch[1])
		// the name will follow by street number
		// split by street number
		re := regexp.MustCompile(`\d`)
		firstNumberIndex := re.FindStringIndex(buyerInfo)
		if firstNumberIndex != nil {
			buyerName = strings.TrimSpace(buyerInfo[:firstNumberIndex[0]])
			buyerAddress = strings.TrimSpace(buyerInfo[firstNumberIndex[0]:])
		} else {
			// if buyer address started with letter, its not gonna detect
			buyerName = buyerInfo
		}
	}

	// buyer phone
	buyerPhonePattern := regexp.MustCompile(`Phone:\s*(.*?)\s*#`)
	buyerPhoneMatch := buyerPhonePattern.FindStringSubmatch(text)
	var buyerPhone string
	if len(buyerPhoneMatch) > 1 {
		buyerPhone = strings.TrimSpace(buyerPhoneMatch[1])
		buyerPhone = strings.ReplaceAll(buyerPhone, "-", "")
		buyerPhone = strings.ReplaceAll(buyerPhone, " ", "")
	}

	// buyer email
	// buyerEmailPattern := regexp.MustCompile(`SHIP TO:\s*(\S+@[^ ]+?\.com)`)
	buyerEmailPattern := regexp.MustCompile(`SHIP TO:\s*(.*?)Lot#`)
	buyerEmailMatch := buyerEmailPattern.FindStringSubmatch(text)
	var buyerEmail string
	if len(buyerEmailMatch) > 1 {
		buyerEmail = strings.TrimSpace(buyerEmailMatch[1])
	}

	// Extract items
	var items []InvoiceItem
	itemPattern := regexp.MustCompile(`MSRP:\$\s*([\d.]+)\s+Y\d+\s+(\d+)T.*?Item handling fee -\s*([\d.]+)`)
	itemMatches := itemPattern.FindAllStringSubmatch(text, -1)
	for _, match := range itemMatches {
		unitPrice, _ := strconv.ParseFloat(match[1], 64)
		sku, _ := strconv.Atoi(match[2])
		itemHandlingFee, _ := strconv.ParseFloat(match[3], 64)
		items = append(items, InvoiceItem{
			Sku:           sku,
			ExtendedPrice: float32(unitPrice),
			Desc:          "", // pull from db
			HandlingFee:   float32(itemHandlingFee),
		})
	}

	return Invoice{
		AuctionLot:    auctionLot,
		BuyerName:     buyerName,
		Items:         items,
		Time:          isoTime,
		BuyerEmail:    buyerEmail,
		BuyerAddress:  buyerAddress,
		BuyerPhone:    buyerPhone,
		InvoiceNumber: invoiceNumber,
	}
}

// func parseInvoiceWithGPT(text string) (Invoice, error) {
// 	ctx := context.Background()
// 	var newInvoice Invoice
// 	// pull open ai key
// 	openAIKey := os.Getenv("OPENAI_API_KEY")
// 	if openAIKey == "" {
// 		return newInvoice, errors.New("cannot get chat gpt key")
// 	}

// 	// create gpt client
// 	client := openai.NewClient(openAIKey)

// 	// create prompt
// 	prompt := "Convert the following text extracted from a PDF document into an array of objects with appropriate fields:\n\n"
// 	prompt += text
// 	prompt += `
// 	export type Invoice = {
// 		invoiceNumber: number,
// 		buyerName: string,
// 		buyerEmail: string,
// 		buyerAddress: string,
// 		paymentMethod: PaymentMethod,
// 		auctionLot: number,
// 		invoiceTotal: number,
// 		buyersPremium: number,
// 		totalHandlingFee: number,
// 		status: InvoiceStatus,
// 		isShipping: boolean,
// 		time: string,
// 		timePickedup: string,
// 		items: InvoiceItem[],
// 	} \n

// 	export type InvoiceItem = {
// 		sku: number
// 		unit: number,
// 		unitPrice: number,
// 		extendedPrice: number,
// 		handlingFee: number,
// 	} \n`

// 	prompt += `
// 	when the input is: \n
// 	"        1      16105Auction Sale - 132 - HIGH/VALUE BOXES/BULK/ELECTRONIC(132)2023-09-25 10:37:54Invoice #:Date:Page:UNPAID2023-09-25 payment declined **2601Grace RoyGrace1600-2300 Young StreetToronto, ON M4P1E4CanadaPhone:647-773-1253# 7104SOLD TO:julius_roy@msn.comLot#DESCRIPTIONQUANTITYUNIT PRICEEXTENDEDPRICE1 x 5.00        5.00Farm Innovators Model HPFLITTLE CRACKED - UNTEST - Farm Innovators Model HPF-100"All-Seasons" Heated Plastic Poultry Fountain, 3 Gallon, Red/White,100-Watt MSRP:$ 74.96 K22 1976T528Item handling fee -         1.00 T  ------------------------------------1 x 5.00        5.00Trampoline Frame Size Replacement NettingN1-1216100000 12 ft. Trampoline Frame Size Replacement NettingMSRP:$ 99.99 H12 10854T788Item handling fee -         1.00 T  ------------------------------------         10.00         1.50Total Extended Price:15% Buyer's Premium:Item handling fee:           2.00          2.00Total Quantity:        1.76Tax1  Default:        $15.26        $15.26Invoice Total:Remaining Invoice Balance:                                  FOR ALL SOLD AS IS ITEMS" \n
// 	the output should be exactly like with only one object inside the array: \n
// 		[{
// 			"invoiceNumber": 16105,
// 			"buyerName": "Grace Roy",
// 			"buyerEmail": "julius_roy@msn.com",
// 			"buyerAddress": "1600-2300 Young Street, Toronto, ON M4P1E4, Canada",
// 			"shippingAddress": "",
// 			"paymentMethod": "",
// 			"auctionLot": 132,
// 			"invoiceTotal": 15.26,
// 			"buyersPremium": 1.50,
// 			"totalHandlingFee": 2.00,
// 			"status": "UNPAID",
// 			"isShipping": false,
// 			"time": "2023-09-25T10:37:54",
// 			"timePickedup": "",
// 			"items": [
// 				{
// 					"sku": 1976T528,
// 					"unit": 1,
// 					"unitPrice": 5.00,
// 					"extendedPrice": 5.00,
// 					"handlingFee": 1.00
// 				},
// 				{
// 					"sku": 10854T788,
// 					"unit": 1,
// 					"unitPrice": 5.00,
// 					"extendedPrice": 5.00,
// 					"handlingFee": 1.00
// 				}
// 			]
// 		}]
// 	`
// 	prompt += `If the input string contains "SHIP TO", there are two addresses in there, the first one is "BuyerAddress", the second one is "ShippingAddress", if not set ShippingAddress to nil ("") \n`
// 	prompt += `the SKU will be both "1976T528" where the input is "1976T528Item handling fee" \n`
// 	prompt += `"paymentMethod" should be empty string ("") if UNPAID mentioned in the input \n`
// 	prompt += `the result JSON Array should ONLY have one Invoice object that contains items from all pages \n`

// 	// call gpt api
// 	res, err := client.CreateChatCompletion(
// 		ctx,
// 		openai.ChatCompletionRequest{
// 			Model:       openai.GPT3Dot5Turbo,
// 			MaxTokens:   4000,
// 			Temperature: 0,
// 			Messages: []openai.ChatCompletionMessage{{
// 				Role:    openai.ChatMessageRoleUser,
// 				Content: prompt,
// 			}},
// 		},
// 	)
// 	if err != nil {
// 		return newInvoice, errors.New("cannot get gpt result")
// 	}
// 	fmt.Println(res.Choices[0].Message.Content)
// 	return newInvoice, nil
// }

// this one only process UNPAID invoice pdf
func CreateInvoiceFromPDF(storageClient *minio.Client, mongoClient *mongo.Collection) gin.HandlerFunc {
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

				// upload invoice to space object storage if uploadPDF in form is true
				if toUpload {
					// check if bucket exist
					exists, existErr := storageClient.BucketExists(ctx, "Invoices")
					if existErr != nil || !exists {
						c.String(http.StatusInternalServerError, "Bucket Not Exist")
						return
					}
					cdnLink := UploadInvoice(ctx, storageClient, file, fileHeader)
					fmt.Println(cdnLink)
				}

				// create temp file from buffer
				tmp, createErr := os.CreateTemp("./", "*.pdf")
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
				fillErr := FillItemDataFromDB(&invoice, mongoClient)
				if fillErr != nil {
					fmt.Println(fillErr)
				}
				invoices = append(invoices, invoice)
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
		fmt.Println("newInvoice:")
		fmt.Println(newInvoice)

		// in, err := strconv.ParseInt(newInvoice.InvoiceNumber, 0, 32)
		// if err != nil {
		// 	c.String(400, "Cannot Convert Invoice Number to Int")
		// }

		// find and update
		res := collection.FindOneAndUpdate(
			ctx,
			bson.M{
				"auctionLot": newInvoice.AuctionLot,
				"buyerName":  newInvoice.BuyerName,
				"time":       newInvoice.Time,
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
		fmt.Println(words)
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

		// make mongodb query filter
		andFilters := bson.A{
			shippingFilter,
			invoiceNumberFilter,
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
		fmt.Println(fil)

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

// datas for invoice controller charts
func GetChartData(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {

	}
}

// convert all time strings to RFC3339 format
// func ConvertAllTimes(collection *mongo.Collection) gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		ctx := context.Background()
// 		cursor, err := collection.Find(ctx, bson.M{"time": bson.M{"$exists": true}}, nil)
// 		if err != nil {
// 			log.Fatal("cannot find")
// 		}
// 		defer cursor.Close(ctx)
// 		for cursor.Next(ctx) {
// 			var document bson.M
// 			err := cursor.Decode(&document)
// 			if err != nil {
// 				log.Fatal("cannot decode cursor")
// 			}
// 			parsedTime, err := time.Parse("1/2/2006, 3:04:05 PM", document["time"].(string))
// 			if err != nil {
// 				log.Fatal("cannot parse time")
// 			}
// 			newTime := parsedTime.Format(timeFormat)
// 			fmt.Println(newTime)
// 			updateOption := bson.M{
// 				"$set": bson.M{
// 					"time": newTime,
// 				},
// 			}
// 			_, updateErr := collection.UpdateOne(ctx, bson.M{"_id": document["_id"]}, updateOption)
// 			if updateErr != nil {
// 				log.Fatal("Cannot update")
// 			}
// 		}
// 		c.String(200, "Pass")
// 	}
// }
