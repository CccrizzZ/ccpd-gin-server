package contactUs

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)
	
type ContactUsForm struct {
    name string
    email  string
	msg string
	time string
	invoice int
}

func SubmitContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {

		// get and destruct json form body
		jsonData, err := io.ReadAll(c.Request.Body)
		if err != nil {
			fmt.Printf("Invalid Body")
		}

		// json data mapping to struct
		newForm := ContactUsForm{
			"Chris",
			"xxx@gamil.com",
			"Please issue me a refund, the goods are faulty",
			time.Now().Format("RFC1123Z"),
			123123,
		}
		fmt.Println(newForm)

		var result bson.M
		findMsg := collection.FindOne(context.TODO(), bson.D{{}}).Decode(&result)
		if findMsg == mongo.ErrNoDocuments {
			fmt.Println("No document found")
			return
		}
		if err != nil {
			panic(err)
		}
		// return json data
		c.JSON(200, jsonData)
	}
}

func GetContactForm(collection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		fmt.Println(time.Now().Format("RFC1123Z"))
		
		// invoke mongo db
		var result bson.M
		// err := collection.FindOne(context.TODO(), bson.D{{
		// 	"name",
		// 	"Christopher Liu",
		// }}).Decode(&result)
		// if err == mongo.ErrNoDocuments {
		// 	fmt.Println("No document found")
		// 	return
		// }
		// if err != nil {
		// 	panic(err)
		// }


		c.JSON(200, result) 
    }
}