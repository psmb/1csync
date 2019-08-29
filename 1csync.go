package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/fatih/color"
	"github.com/joho/godotenv"
	"github.com/mozillazg/go-slugify"
)

var _syliusToken string

var _prices map[string]interface{}

func logVerbose(value interface{}) {
	argsWithoutProg := os.Args[1:]
	if len(argsWithoutProg) > 0 {
		if argsWithoutProg[0] == "-v" {
			fmt.Println(value)
		}
	}
}

func syncPrices() {
	url := "/1cbooks/odata/standard.odata/InformationRegister_%D0%A6%D0%B5%D0%BD%D1%8B%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D1%8B/?$format=json"

	pricesR := odinCRequest("GET", url, nil)
	for _, readersRaw := range pricesR["value"].([]interface{}) {
		readers := readersRaw.(map[string]interface{})
		for _, priceItemRaw := range readers["RecordSet"].([]interface{}) {
			priceItem := priceItemRaw.(map[string]interface{})
			productCode := priceItem["Номенклатура_Key"].(string)
			if priceItem["ВидЦены_Key"].(string) == "a0965697-a587-11e6-8857-14dae924f847" {
				if savedItemR, ok := _prices[productCode]; ok {
					savedItem := savedItemR.(map[string]interface{})
					currentDate, _ := time.Parse(time.RFC3339, priceItem["Period"].(string)+"Z")
					savedDate, _ := time.Parse(time.RFC3339, savedItem["Period"].(string)+"Z")
					if currentDate.After(savedDate) {
						_prices[productCode] = priceItem
					}
				} else {
					_prices[productCode] = priceItem
				}
			}
		}
	}
}

func syncManufacturers() {
	url := "/1cbooks/odata/standard.odata/Catalog_%D0%9F%D1%80%D0%BE%D0%B8%D0%B7%D0%B2%D0%BE%D0%B4%D0%B8%D1%82%D0%B5%D0%BB%D0%B8/?$format=json"

	manufacterersR := odinCRequest("GET", url, nil)
	for _, manufacturerRaw := range manufacterersR["value"].([]interface{}) {
		manufacterer := manufacturerRaw.(map[string]interface{})
		code := manufacterer["Ref_Key"].(string)
		name := manufacterer["Description"].(string)
		body, _ := json.Marshal(map[string]interface{}{
			"code":   code,
			"parent": "publishers",
			"translations": map[string]interface{}{
				"ru_RU": map[string]string{
					"name": name,
					"slug": "category/publishers/" + slugify.Slugify(name),
				},
			},
		})
		syliusRequest("PUT", "/api/v1/taxons/"+code, bytes.NewReader(body), "application/json")
	}
}

var _existingAuthors map[string]bool

func getAuthorTaxon(name string) string {
	code := slugify.Slugify(name)
	if _existingAuthors[code] {
		return code
	}
	body, _ := json.Marshal(map[string]interface{}{
		"code":   code,
		"parent": "authors",
		"translations": map[string]interface{}{
			"ru_RU": map[string]string{
				"name": name,
				"slug": "category/authors/" + code,
			},
		},
	})
	logVerbose("Creating author: " + code)
	syliusRequest("PUT", "/api/v1/taxons/"+code, bytes.NewReader(body), "application/json")
	_existingAuthors[code] = true
	return code
}

var validCategories map[string]bool

func syncCategories() {
	url := "/1cbooks/odata/standard.odata/Catalog_%D0%97%D0%BD%D0%B0%D1%87%D0%B5%D0%BD%D0%B8%D1%8F%D0%A1%D0%B2%D0%BE%D0%B9%D1%81%D1%82%D0%B2%D0%9E%D0%B1%D1%8A%D0%B5%D0%BA%D1%82%D0%BE%D0%B2%D0%98%D0%B5%D1%80%D0%B0%D1%80%D1%85%D0%B8%D1%8F/?$format=json"

	catgoriesR := odinCRequest("GET", url, nil)
	validCategories = make(map[string]bool)
	for _, categoriesRaw := range catgoriesR["value"].([]interface{}) {
		category := categoriesRaw.(map[string]interface{})
		code := category["Ref_Key"].(string)
		parentKey := category["Parent_Key"].(string)
		name := category["Description"].(string)
		if parentKey == "79da890c-ac54-11e9-a4b0-08606ed6b998" {
			body, _ := json.Marshal(map[string]interface{}{
				"code":   code,
				"parent": "books",
				"translations": map[string]interface{}{
					"ru_RU": map[string]string{
						"name": name,
						"slug": "category/books/" + slugify.Slugify(name),
					},
				},
			})
			syliusRequest("PUT", "/api/v1/taxons/"+code, bytes.NewReader(body), "application/json")
			validCategories[code] = true
		}
	}
}

func fetchSyliusToken() {
	syliusHost, _ := os.LookupEnv("SYLIUS_HOST")
	link := syliusHost + "/api/oauth/v2/token"

	syliusClientID, _ := os.LookupEnv("SYLIUS_CLIENT_ID")
	syliusClientSecret, _ := os.LookupEnv("SYLIUS_CLIENT_SECRET")
	syliusAPIUsername, _ := os.LookupEnv("SYLIUS_API_USERNAME")
	syliusAPIPassword, _ := os.LookupEnv("SYLIUS_API_PASSWORD")

	formData := url.Values{
		"client_id":     {syliusClientID},
		"client_secret": {syliusClientSecret},
		"grant_type":    {"password"},
		"username":      {syliusAPIUsername},
		"password":      {syliusAPIPassword},
	}

	resp, err := http.PostForm(link, formData)

	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var decodedBody map[string]interface{}
	errJSON := json.Unmarshal(body, &decodedBody)
	if errJSON != nil {
		panic(errJSON)
	}

	_syliusToken = decodedBody["access_token"].(string)
}

func initApp() {
	// loads values from .env into the system
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}

	_existingAuthors = make(map[string]bool)
	_prices = make(map[string]interface{})

	fetchSyliusToken()
	syncPrices()
	syncManufacturers()
	syncCategories()
}

func syliusRequest(requestType string, url string, body io.Reader, contentType string) map[string]interface{} {
	syliusHost, _ := os.LookupEnv("SYLIUS_HOST")
	req, errRequest := http.NewRequest(requestType, syliusHost+url, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+_syliusToken)
	client := &http.Client{}
	if errRequest != nil {
		panic(errRequest)
	}
	resp, errResp := client.Do(req)
	if errResp != nil {
		panic(errResp)
	}
	defer resp.Body.Close()
	respBody, errReadAll := ioutil.ReadAll(resp.Body)
	if errReadAll != nil {
		panic(errReadAll)
	}
	var decodedBody map[string]interface{}
	errJSON := json.Unmarshal(respBody, &decodedBody)
	if resp.StatusCode >= 500 && requestType != "DELETE" {
		color.Magenta("PANIC!")
		fmt.Println(req)
		spew.Dump(decodedBody)
		panic(decodedBody)
	}
	if errJSON != nil {
		return map[string]interface{}{
			"body": respBody,
		}
	}
	return decodedBody
}

func syliusPutRequest(url string, slug string, body io.Reader, contentType string) map[string]interface{} {
	resourceExists := syliusRequest("GET", url+slug, nil, "application/json")
	if resourceExists["code"] == 404.00 {
		logVerbose("Creating new: " + url + ";" + slug)
		return syliusRequest("POST", url, body, contentType)
	}
	logVerbose("Updating: " + url + ";" + slug)
	return syliusRequest("PATCH", url+slug, body, contentType)
}

func odinCRequest(requestType string, url string, body io.Reader) map[string]interface{} {
	host, _ := os.LookupEnv("1C_HOST")
	req, errRequest := http.NewRequest(requestType, host+url, body)
	req.Header.Set("Content-Type", "application/json")
	odinCLogin, _ := os.LookupEnv("1C_LOGIN")
	odinCPassword, _ := os.LookupEnv("1C_PASSWORD")
	req.SetBasicAuth(odinCLogin, odinCPassword)
	client := &http.Client{}
	if errRequest != nil {
		panic(errRequest)
	}
	resp, errResp := client.Do(req)
	if errResp != nil {
		panic(errResp)
	}
	defer resp.Body.Close()
	respBody, errReadAll := ioutil.ReadAll(resp.Body)
	if errReadAll != nil {
		panic(errReadAll)
	}
	var decodedBody map[string]interface{}
	errJSON := json.Unmarshal(respBody, &decodedBody)
	if errJSON != nil {
		panic(errJSON)
	}
	return decodedBody
}

func importProduct(sourceProduct map[string]interface{}) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
		}
	}()
	sourceProductID := sourceProduct["Ref_Key"].(string)
	slug := sourceProduct["Артикул"].(string)

	logVerbose("=== Importing product: " + slug + "===")

	type productAttribute map[string]string

	var productTaxons []string
	var productAttributes []productAttribute
	mainTaxon := ""
	weight := ""
	width := ""
	height := ""
	depth := ""
	manufacturerKey := sourceProduct["Производитель_Key"].(string)
	if len(manufacturerKey) > 0 && manufacturerKey != "00000000-0000-0000-0000-000000000000" {
		productTaxons = append(productTaxons, sourceProduct["Производитель_Key"].(string))
	}
	dops := sourceProduct["ДополнительныеРеквизиты"].([]interface{})

	for _, dopRaw := range dops {
		dop := dopRaw.(map[string]interface{})
		if dop["Свойство_Key"].(string) == "52f8b02d-552e-11e9-907f-14dae924f847" && validCategories[dop["Значение"].(string)] {
			productTaxons = append(productTaxons, dop["Значение"].(string))
			mainTaxon = dop["Значение"].(string)
		}
		if dop["Свойство_Key"].(string) == "39c57eb5-5016-11e7-89aa-3085a93bff67" {
			authorNamesString := dop["Значение"].(string)
			authorNames := strings.Split(authorNamesString, "===")
			for _, authorName := range authorNames {
				productTaxons = append(productTaxons, getAuthorTaxon(authorName))
			}
		}
		if dop["Свойство_Key"].(string) == "39c57eb4-5016-11e7-89aa-3085a93bff67" {
			var attribute = map[string]string{
				"attribute":  "isbn",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64bacc-c8b8-11e9-94d8-08606ed6b998" {
			var attribute = map[string]string{
				"attribute":  "sostavitel",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64bace-c8b8-11e9-94d8-08606ed6b998" {
			var attribute = map[string]string{
				"attribute":  "redactor",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "2270db75-ad8e-11e6-907d-14dae924f847" {
			var attribute = map[string]string{
				"attribute":  "perevodchik",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64bad2-c8b8-11e9-94d8-08606ed6b998" {
			var attribute = map[string]string{
				"attribute":  "pages",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64bad6-c8b8-11e9-94d8-08606ed6b998" {
			var attribute = map[string]string{
				"attribute":  "cover_type",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64badc-c8b8-11e9-94d8-08606ed6b998" {
			var attribute = map[string]string{
				"attribute":  "recommendation",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "3a64bad8-c8b8-11e9-94d8-08606ed6b998" {
			dimensionsString := dop["Значение"].(string)
			dimensions := strings.Split(dimensionsString, "х")
			if len(dimensions) == 3 {
				width = dimensions[0]
				height = dimensions[1]
				depth = dimensions[2]
			}
		}
		if dop["Свойство_Key"].(string) == "3a64bada-c8b8-11e9-94d8-08606ed6b998" {
			weight = dop["Значение"].(string)
		}
	}

	productData := map[string]interface{}{
		"code":    slug,
		"enabled": true,
		"translations": map[string]interface{}{
			"ru_RU": map[string]string{
				"name":             sourceProduct["НаименованиеЗаголовок"].(string),
				"shortDescription": sourceProduct["НаименованиеПодаголовок"].(string),
				"description":      sourceProduct["Описание"].(string),
				"slug":             slug,
			},
		},
		"attributes": productAttributes,
		"channels":   []string{"default"},
	}
	productTaxonsString := strings.Join(productTaxons, ",")
	if len(productTaxonsString) > 0 {
		productData["productTaxons"] = productTaxonsString
	}
	if len(mainTaxon) > 0 {
		productData["mainTaxon"] = mainTaxon
	}

	resourceExists := syliusRequest("GET", "/api/v1/products/"+slug+"/variants/"+slug, nil, "application/json")
	if resourceExists["code"] != 404.00 {
		logVerbose("Adding onhand fix")
		variant := map[string]interface{}{
			"translations": map[string]interface{}{
				"ru_RU": map[string]string{
					"name": "Dummy variant to prevent the error",
				},
			},
			"channelPricings": map[string]interface{}{
				"default": map[string]float64{
					"price": 123.00,
				},
			},
			"onHand": 123.00,
		}
		if weight != "" {
			variant["weight"] = weight
		}
		if width != "" {
			variant["width"] = width
		}
		if height != "" {
			variant["height"] = height
		}
		if depth != "" {
			variant["depth"] = depth
		}
		productData["variant"] = variant
	}

	productBody, _ := json.Marshal(productData)

	result := syliusPutRequest("/api/v1/products/", slug, bytes.NewReader(productBody), "application/json")

	if val, ok := result["errors"]; ok {
		color.Red("ERROR product!")
		fmt.Println(productData["productTaxons"])
		fmt.Println(val)
	} else {
		if priceItem, ok := _prices[sourceProduct["Ref_Key"].(string)]; ok {
			price := priceItem.(map[string]interface{})["Цена"].(float64)
			variantBody, _ := json.Marshal(map[string]interface{}{
				"code":             slug,
				"tracked":          false,
				"shippingRequired": true,
				"translations": map[string]interface{}{
					"ru_RU": map[string]string{
						"name": "бумажный вариант",
					},
				},
				"channelPricings": map[string]interface{}{
					"default": map[string]float64{
						"price": price,
					},
				},
			})
			variantsResult := syliusPutRequest("/api/v1/products/"+slug+"/variants/", slug, bytes.NewReader(variantBody), "application/json")
			if val, ok := variantsResult["errors"]; ok {
				color.Red("ERROR variants!")
				fmt.Println(val)
			}
		} else {
			color.Red("ERROR! Price not available")
		}

		logVerbose("Get images")
		imagesRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0%D0%9F%D1%80%D0%B8%D1%81%D0%BE%D0%B5%D0%B4%D0%B8%D0%BD%D0%B5%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D1%8B/?$format=json&%24filter=%D0%92%D0%BB%D0%B0%D0%B4%D0%B5%D0%BB%D0%B5%D1%86%D0%A4%D0%B0%D0%B9%D0%BB%D0%B0_Key%20eq%20guid%27"+sourceProductID+"%27", nil)
		images := imagesRaw["value"].([]interface{})
		isFirst := true
		imagesData := map[string]interface{}{}
		for i, imageRaw := range images {
			index := strconv.Itoa(i)
			imageRaw := imageRaw.(map[string]interface{})
			imageID := imageRaw["Ref_Key"].(string)
			ext := imageRaw["Расширение"].(string)
			logVerbose("- Get image data")
			imageDataRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/InformationRegister_%D0%94%D0%B2%D0%BE%D0%B8%D1%87%D0%BD%D1%8B%D0%B5%D0%94%D0%B0%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D0%BE%D0%B2(%D0%A4%D0%B0%D0%B9%D0%BB='"+imageID+"',%20%D0%A4%D0%B0%D0%B9%D0%BB_Type='StandardODATA.Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0%D0%9F%D1%80%D0%B8%D1%81%D0%BE%D0%B5%D0%B4%D0%B8%D0%BD%D0%B5%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D1%8B')/?$format=json", nil)
			base64image := imageDataRaw["ДвоичныеДанныеФайла_Base64Data"].(string)
			base64imageFixed := strings.ReplaceAll(base64image, "\r\n", "")
			imageData, _ := base64.StdEncoding.DecodeString(base64imageFixed)

			var imageType string
			if ext == "pdf" {
				imageType = "pdf"
			} else if isFirst {
				isFirst = false
				imageType = "main"
			} else {
				imageType = "default"
			}

			imagesData["images["+index+"][file]"] = imageData
			imagesData["images["+index+"][type]"] = imageType
		}
		body, contentType := makeMultipartBody(imagesData)
		imagesResult := syliusRequest("POST", "/api/v1/products/"+slug+"?_method=PATCH", body, contentType)
		if val, ok := imagesResult["errors"]; ok {
			color.Red("ERROR images!")
			fmt.Println(val)
		}
	}

}

func main() {
	fmt.Println("Syncing 1C and Sylius")
	initApp()

	logVerbose("Get products from 1C")
	productsRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20ne%20%27%27", nil)
	// productsRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20eq%20%27prayers%27", nil)
	products := productsRaw["value"].([]interface{})
	for _, productRaw := range products {

		sourceProduct := productRaw.(map[string]interface{})
		importProduct(sourceProduct)
	}
	fmt.Println("Done!")
}

func makeMultipartBody(values map[string]interface{}) (body io.Reader, contentType string) {
	var buffer bytes.Buffer
	var err error
	multipartWriter := multipart.NewWriter(&buffer)
	for key, r := range values {
		var writer io.Writer
		switch v := r.(type) {
		case string:
			if writer, err = multipartWriter.CreateFormField(key); err != nil {
				panic(err)
			}
			if _, err = io.Copy(writer, strings.NewReader(v)); err != nil {
				panic(err)
			}
		case bool:
			if writer, err = multipartWriter.CreateFormField(key); err != nil {
				panic(err)
			}
			if _, err = io.Copy(writer, strings.NewReader(strconv.FormatBool(v))); err != nil {
				panic(err)
			}

		case float64:
			if writer, err = multipartWriter.CreateFormField(key); err != nil {
				panic(err)
			}
			if _, err = io.Copy(writer, strings.NewReader(strconv.FormatFloat(v, 'g', -1, 64))); err != nil {
				panic(err)
			}
		default:
			if writer, err = multipartWriter.CreateFormFile(key, randString(8)+".jpg"); err != nil {
				panic(err)
			}
			if _, err = io.Copy(writer, bytes.NewReader(v.([]byte))); err != nil {
				panic(err)
			}
		}

	}
	multipartWriter.Close()

	return bytes.NewReader(buffer.Bytes()), multipartWriter.FormDataContentType()
}

func randString(n int) string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
