package main

import (
	"bytes"
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

var variantTypes = map[string]interface{}{
	"default": map[string]interface{}{
		"shippingRequired": true,
		"title":            "бумажная книга",
	},
	"ebook": map[string]interface{}{
		"shippingRequired": false,
		"title":            "электронная книга",
	},
	"audio": map[string]interface{}{
		"shippingRequired": false,
		"title":            "аудио книга",
	},
	"video": map[string]interface{}{
		"shippingRequired": false,
		"title":            "аудио книга",
	},
}

var _syliusToken string

var _prices map[string]interface{}

var _values map[string]interface{}

var _manufacturers map[string]interface{}

var _variants map[string][]map[string]interface{}

func logVerbose(value interface{}) {
	argsWithoutProg := os.Args[1:]
	if len(argsWithoutProg) > 0 {
		if argsWithoutProg[0] == "-v" {
			fmt.Println(value)
		}
	}
}

func syncPrices() {
	url := "/odata/standard.odata/InformationRegister_%D0%A6%D0%B5%D0%BD%D1%8B%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D1%8B/?$format=json"

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

func fetchValues() {
	url := "/odata/standard.odata/Catalog_%D0%97%D0%BD%D0%B0%D1%87%D0%B5%D0%BD%D0%B8%D1%8F%D0%A1%D0%B2%D0%BE%D0%B9%D1%81%D1%82%D0%B2%D0%9E%D0%B1%D1%8A%D0%B5%D0%BA%D1%82%D0%BE%D0%B2/?$format=json"

	valuesR := odinCRequest("GET", url, nil)
	for _, valuesR := range valuesR["value"].([]interface{}) {
		valueItemRaw := valuesR.(map[string]interface{})
		valueItem := valueItemRaw
		ref := valueItem["Ref_Key"].(string)
		name := valueItem["Description"].(string)
		_values[ref] = name
	}
}
func fetchManufacturers() {
	url := "/odata/standard.odata/Catalog_%D0%9F%D1%80%D0%BE%D0%B8%D0%B7%D0%B2%D0%BE%D0%B4%D0%B8%D1%82%D0%B5%D0%BB%D0%B8/?$format=json"

	valuesR := odinCRequest("GET", url, nil)
	for _, valuesR := range valuesR["value"].([]interface{}) {
		valueItemRaw := valuesR.(map[string]interface{})
		valueItem := valueItemRaw
		ref := valueItem["Ref_Key"].(string)
		name := valueItem["Description"].(string)
		_manufacturers[ref] = name
	}
}

var _importedAuthors map[string]bool

func pruneAuthors() {
	existingAuthors := syliusRequest("GET", "/api/v1/taxons/authors", nil, "application/json")
	for _, authorItem := range existingAuthors["children"].([]interface{}) {
		slug := authorItem.(map[string]interface{})["code"].(string)
		if !_importedAuthors[slug] {
			syliusRequest("DELETE", "/api/v1/taxons/"+slug, nil, "application/json")
			logVerbose("Deleted author " + slug)
		}
	}
}

func getAuthorTaxon(name string) string {
	code := slugify.Slugify(name)
	if _importedAuthors[code] {
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
	resp := syliusPutRequest("/api/v1/taxons/", code, bytes.NewReader(body), "application/json")
	if val, ok := resp["errors"]; ok {
		color.Red("ERROR author!")
		spew.Dump(val)
	}
	_importedAuthors[code] = true
	return code
}

var _importedManufacturers map[string]bool

func pruneManufacturers() {
	existingManufacturers := syliusRequest("GET", "/api/v1/taxons/publishers", nil, "application/json")
	for _, manufacturerItem := range existingManufacturers["children"].([]interface{}) {
		slug := manufacturerItem.(map[string]interface{})["code"].(string)
		if !_importedManufacturers[slug] {
			syliusRequest("DELETE", "/api/v1/taxons/"+slug, nil, "application/json")
			logVerbose("Deleted manufacturer " + slug)
		}
	}
}

func getManufacturerTaxon(ref string) string {
	if val, ok := _manufacturers[ref]; ok {
		name := val.(string)
		code := slugify.Slugify(name)

		if _importedManufacturers[ref] {
			return code
		}

		body, _ := json.Marshal(map[string]interface{}{
			"code":   code,
			"parent": "publishers",
			"translations": map[string]interface{}{
				"ru_RU": map[string]string{
					"name": name,
					"slug": "category/publishers/" + code,
				},
			},
		})
		logVerbose("Creating publisher: " + code)
		resp := syliusPutRequest("/api/v1/taxons/", code, bytes.NewReader(body), "application/json")
		if val, ok := resp["errors"]; ok {
			color.Red("ERROR publisher!")
			spew.Dump(val)
		}
		_importedManufacturers[code] = true
		return code
	}
	fmt.Println("Invalid manufacturer ref", ref)
	panic("Invalid manufacturer ref")
}

var validCategories map[string]bool

func syncCategories() {
	url := "/odata/standard.odata/Catalog_%D0%97%D0%BD%D0%B0%D1%87%D0%B5%D0%BD%D0%B8%D1%8F%D0%A1%D0%B2%D0%BE%D0%B9%D1%81%D1%82%D0%B2%D0%9E%D0%B1%D1%8A%D0%B5%D0%BA%D1%82%D0%BE%D0%B2%D0%98%D0%B5%D1%80%D0%B0%D1%80%D1%85%D0%B8%D1%8F/?$format=json"

	catgoriesR := odinCRequest("GET", url, nil)
	validCategories = make(map[string]bool)
	for _, categoriesRaw := range catgoriesR["value"].([]interface{}) {
		category := categoriesRaw.(map[string]interface{})
		code := category["Ref_Key"].(string)
		parentKey := category["Parent_Key"].(string)
		name := category["Description"].(string)
		if parentKey == "d33bd5fe-38f1-11ea-8177-74d02b904d6f" {
			base := "category/books/"
			parent := "books"
			if name == "Мерч" {
				base = "category/"
				parent = "category"
			}
			body, _ := json.Marshal(map[string]interface{}{
				"code":   code,
				"parent": parent,
				"translations": map[string]interface{}{
					"ru_RU": map[string]string{
						"name": name,
						"slug": base + slugify.Slugify(name),
					},
				},
			})
			resp := syliusPutRequest("/api/v1/taxons/", code, bytes.NewReader(body), "application/json")
			if val, ok := resp["errors"]; ok {
				color.Red("ERROR categories!")
				spew.Dump(val)
			}
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

	_importedAuthors = make(map[string]bool)
	_importedManufacturers = make(map[string]bool)
	_values = make(map[string]interface{})
	_manufacturers = make(map[string]interface{})
	_prices = make(map[string]interface{})
	_variants = make(map[string][]map[string]interface{})

	fetchSyliusToken()
	syncCategories()
	fetchValues()
	syncPrices()
	fetchManufacturers()
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
		productTaxons = append(productTaxons, getManufacturerTaxon(sourceProduct["Производитель_Key"].(string)))
	}
	dops := sourceProduct["ДополнительныеРеквизиты"].([]interface{})

	for _, dopRaw := range dops {
		dop := dopRaw.(map[string]interface{})
		// Category
		if dop["Свойство_Key"].(string) == "52f8b02d-552e-11e9-907f-14dae924f847" && validCategories[dop["Значение"].(string)] {
			productTaxons = append(productTaxons, dop["Значение"].(string))
			mainTaxon = dop["Значение"].(string)
		}
		// Author1
		if dop["Свойство_Key"].(string) == "39c57eb5-5016-11e7-89aa-3085a93bff67" {
			authorRefString := dop["Значение"].(string)
			if val, ok := _values[authorRefString]; ok {
				authorName := val.(string)
				productTaxons = append(productTaxons, getAuthorTaxon(authorName))
			} else {
				fmt.Println("Invalid author value", authorRefString)
			}
		}
		// Author2
		if dop["Свойство_Key"].(string) == "1041e448-b526-11ea-8190-74d02b904d6f" {
			authorRefString := dop["Значение"].(string)
			if val, ok := _values[authorRefString]; ok {
				authorName := val.(string)
				productTaxons = append(productTaxons, getAuthorTaxon(authorName))
			} else {
				fmt.Println("Invalid author value", authorRefString)
			}
		}
		// Author3
		if dop["Свойство_Key"].(string) == "1041e44a-b526-11ea-8190-74d02b904d6f" {
			authorRefString := dop["Значение"].(string)
			if val, ok := _values[authorRefString]; ok {
				authorName := val.(string)
				productTaxons = append(productTaxons, getAuthorTaxon(authorName))
			} else {
				fmt.Println("Invalid author value", authorRefString)
			}
		}
		// ISBN
		if dop["Свойство_Key"].(string) == "39c57eb4-5016-11e7-89aa-3085a93bff67" {
			var attribute = map[string]string{
				"attribute":  "isbn",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		// Sostavitel
		if dop["Свойство_Key"].(string) == "d33bd5eb-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "sostavitel",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		// Redactor
		if dop["Свойство_Key"].(string) == "d33bd5ed-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "redactor",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		// Perevodchik
		if dop["Свойство_Key"].(string) == "d33bd5ef-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "perevodchik",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "d33bd5f1-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "pages",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "d33bd5f3-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "cover_type",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "d33bd5f9-38f1-11ea-8177-74d02b904d6f" {
			var attribute = map[string]string{
				"attribute":  "recommendation",
				"localeCode": "ru_RU",
				"value":      dop["Значение"].(string),
			}
			productAttributes = append(productAttributes, attribute)
		}
		if dop["Свойство_Key"].(string) == "d33bd5f5-38f1-11ea-8177-74d02b904d6f" {
			dimensionsString := dop["Значение"].(string)
			dimensions := strings.Split(dimensionsString, "х")
			if len(dimensions) == 3 {
				width = dimensions[0]
				height = dimensions[1]
				depth = dimensions[2]
			}
		}
		if dop["Свойство_Key"].(string) == "d33bd5f7-38f1-11ea-8177-74d02b904d6f" {
			weight = dop["Значение"].(string)
		}
		// set the discount if originalPrice is set
		if dop["Свойство_Key"].(string) == "d33bd5fd-38f1-11ea-8177-74d02b904d6f" {
			originalPrice, _ := strconv.ParseFloat(dop["Значение"].(string), 64)
			if originalPrice > 0 {
				productTaxons = append(productTaxons, "6ad73508-09dc-11ea-98c8-08606ed6b998")
			}
		}
	}

	publishDate := sourceProduct["ДатаПереиздания"].(string)
	if publishDate == "0001-01-01T00:00:00" {
		publishDate = "1971-01-01T00:00:00"
	}
	publishDateObject, err := time.Parse(time.RFC3339, publishDate+"Z")
	if err != nil {
		fmt.Println("Error while parsing date :", err)
	}
	var datePublish = map[string]string{
		"attribute":  "publish_date",
		"localeCode": "ru_RU",
		"value":      strconv.FormatInt(publishDateObject.Unix(), 10),
	}
	productAttributes = append(productAttributes, datePublish)

	productData := map[string]interface{}{
		"code":    slug,
		"enabled": true,
		"translations": map[string]interface{}{
			"ru_RU": map[string]string{
				"name":             sourceProduct["НаименованиеЗаголовок"].(string),
				"shortDescription": sourceProduct["НаименованиеПодаголовок"].(string),
				"description":      sourceProduct["Описание_Сайт"].(string),
				"slug":             slug,
			},
		},
		"attributes": productAttributes,
		"channels":   []string{"default"},
	}

	if additionalVariants, ok := _variants[slug]; ok {
		for _, variant := range additionalVariants {
			variantSlug := variant["Артикул"].(string)
			if variantSlug == slug+"_ebook" {
				productTaxons = append(productTaxons, "ebooks")
			}
			if variantSlug == slug+"_audio" {
				productTaxons = append(productTaxons, "audio")
			}
			if variantSlug == slug+"_video" {
				productTaxons = append(productTaxons, "video")
			}
		}
	}
	productTaxonsString := strings.Join(productTaxons, ",")
	if len(productTaxonsString) > 0 {
		productData["productTaxons"] = productTaxonsString
	}
	if len(mainTaxon) > 0 {
		productData["mainTaxon"] = mainTaxon
	}

	productBody, _ := json.Marshal(productData)

	result := syliusPutRequest("/api/v1/products/", slug, bytes.NewReader(productBody), "application/json")

	if val, ok := result["errors"]; ok {
		color.Red("ERROR product!")
		fmt.Println(string(productBody))
		spew.Dump(val)
	} else {
		variants := make([]map[string]interface{}, 0)
		variants = append(variants, sourceProduct)
		if additionalVariants, ok := _variants[slug]; ok {
			variants = append(variants, additionalVariants...)
		}

		for _, variant := range variants {
			variantSlug := variant["Артикул"].(string)
			variantID := variant["Ref_Key"].(string)
			splitVariantSlug := strings.Split(variantSlug, "_")
			var variantType string
			if len(splitVariantSlug) == 1 {
				variantType = "default"
			} else if len(splitVariantSlug) == 2 {
				variantType = splitVariantSlug[1]
			} else {
				panic("Too many underscores in the variant: " + variantSlug)
			}
			if _, ok := variantTypes[variantType]; !ok {
				panic("Wrong variant: " + variantSlug)
			}

			var originalPrice float64
			hidden := false
			dops := variant["ДополнительныеРеквизиты"].([]interface{})

			for _, dopRaw := range dops {
				dop := dopRaw.(map[string]interface{})
				if dop["Свойство_Key"].(string) == "d33bd5fd-38f1-11ea-8177-74d02b904d6f" {
					originalPrice, _ = strconv.ParseFloat(dop["Значение"].(string), 64)
				}
				if dop["Свойство_Key"].(string) == "b3ac0624-bc51-11ea-8190-74d02b904d6f" {
					hiddenValue, _ := dop["Значение"].(string)
					if hiddenValue == "true" {
						hidden = true
					}
				}
			}

			if priceItem, ok := _prices[variantID]; ok && priceItem.(map[string]interface{})["Цена"].(float64) > 0.00 {
				variantObject := map[string]interface{}{
					"code":             variantSlug,
					"tracked":          false,
					"shippingRequired": variantTypes[variantType].(map[string]interface{})["shippingRequired"].(bool),
					"translations": map[string]interface{}{
						"ru_RU": map[string]string{
							"name": variantTypes[variantType].(map[string]interface{})["title"].(string),
						},
					},
					"channelPricings": map[string]interface{}{
						"default": map[string]float64{
							"price": priceItem.(map[string]interface{})["Цена"].(float64),
						},
					},
				}
				if hidden {
					variantObject["tracked"] = true
					variantObject["onHand"] = 0
				}
				if variantType == "default" {
					if weight != "" {
						variantObject["weight"] = weight
					}
					if width != "" {
						variantObject["width"] = width
					}
					if height != "" {
						variantObject["height"] = height
					}
					if depth != "" {
						variantObject["depth"] = depth
					}
				}
				if originalPrice > 0 {
					variantObject["channelPricings"].(map[string]interface{})["default"].(map[string]float64)["originalPrice"] = originalPrice
				}
				variantBody, _ := json.Marshal(variantObject)
				variantsResult := syliusPutRequest("/api/v1/products/"+slug+"/variants/", variantSlug, bytes.NewReader(variantBody), "application/json")
				if val, ok := variantsResult["errors"]; ok {
					color.Red("ERROR variants!")
					fmt.Println(val)
				}
			} else {
				syliusRequest("DELETE", "/api/v1/products/"+slug+"/variants/"+variantSlug, nil, "application/json")
				color.Yellow("Price not available, deleted variant")
			}

		}
	}
}

func main() {
	fmt.Println("Syncing 1C and Sylius")
	initApp()

	_existingProducts := make([]string, 0)
	existingProducts := syliusRequest("GET", "/api/v1/products/?limit=1000", nil, "application/json")
	for _, product := range existingProducts["_embedded"].(map[string]interface{})["items"].([]interface{}) {
		_existingProducts = append(_existingProducts, product.(map[string]interface{})["code"].(string))
	}

	logVerbose("Get products from 1C")
	_newProducts := make([]string, 0)
	products := make([]interface{}, 0)
	productsAndVariantsRaw := odinCRequest("GET", "/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20ne%20%27%27&$orderby=%D0%94%D0%B0%D1%82%D0%B0%D0%9F%D0%B5%D1%80%D0%B5%D0%B8%D0%B7%D0%B4%D0%B0%D0%BD%D0%B8%D1%8F%20asc", nil)
	// productsAndVariantsRaw := odinCRequest("GET", "/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20eq%20%27ethics-10%27", nil)
	productsAndVariants := productsAndVariantsRaw["value"].([]interface{})
	for _, productRaw := range productsAndVariants {
		sourceProduct := productRaw.(map[string]interface{})
		slug := sourceProduct["Артикул"].(string)
		subparts := strings.Split(slug, "_")
		if len(subparts) == 2 {
			productSlug := subparts[0]
			if _, ok := _variants[productSlug]; !ok {
				_variants[productSlug] = make([]map[string]interface{}, 0)
			}
			_variants[productSlug] = append(_variants[productSlug], sourceProduct)
		} else {
			products = append(products, sourceProduct)
		}
	}
	for _, productRaw := range products {
		sourceProduct := productRaw.(map[string]interface{})
		slug := sourceProduct["Артикул"].(string)
		importProduct(sourceProduct)
		_newProducts = append(_newProducts, slug)
	}

	for _, slug := range _existingProducts {
		if !containsString(_newProducts, slug) {
			body, _ := json.Marshal(map[string]interface{}{
				"enabled": false,
			})
			syliusRequest("PATCH", "/api/v1/products/"+slug, bytes.NewReader(body), "application/json")
			logVerbose("Disabled " + slug)
		}
	}

	pruneAuthors()
	pruneManufacturers()
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

func containsString(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
