package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
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

	"github.com/fatih/color"
	"github.com/joho/godotenv"
	"github.com/mozillazg/go-slugify"
)

var _syliusToken string

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

var existingAuthors map[string]bool

func getAuthorTaxon(name string) string {
	code := slugify.Slugify(name)
	if existingAuthors[code] {
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
	fmt.Println("Creating author: " + code)
	syliusRequest("PUT", "/api/v1/taxons/"+code, bytes.NewReader(body), "application/json")
	existingAuthors[code] = true
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

	existingAuthors = make(map[string]bool)

	fetchSyliusToken()
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
		fmt.Println(decodedBody)
		panic(decodedBody)
	}
	if errJSON != nil {
		return map[string]interface{}{
			"body": respBody,
		}
	}
	return decodedBody
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
	slug := strings.Replace(sourceProduct["Артикул"].(string), " ", "-", -1)

	fmt.Println("=== Importing product: " + slug + "===")

	// Delete existing product as we can't PUT yet
	// @see: https://github.com/Sylius/Sylius/issues/10532
	syliusRequest("DELETE", "/api/v1/products/"+slug, nil, "application/json")

	var productTaxons []string
	manufacturerKey := sourceProduct["Производитель_Key"].(string)
	if len(manufacturerKey) > 0 && manufacturerKey != "00000000-0000-0000-0000-000000000000" {
		productTaxons = append(productTaxons, sourceProduct["Производитель_Key"].(string))
	}
	dops := sourceProduct["ДополнительныеРеквизиты"].([]interface{})

	for _, dopRaw := range dops {
		dop := dopRaw.(map[string]interface{})
		if dop["Свойство_Key"].(string) == "52f8b02d-552e-11e9-907f-14dae924f847" && validCategories[dop["Значение"].(string)] {
			productTaxons = append(productTaxons, dop["Значение"].(string))
		}
		if dop["Свойство_Key"].(string) == "39c57eb5-5016-11e7-89aa-3085a93bff67" {
			authorName := dop["Значение"].(string)
			productTaxons = append(productTaxons, getAuthorTaxon(authorName))
		}
	}

	productData := map[string]interface{}{
		"code":                             slug,
		"enabled":                          true,
		"channels[0]":                      "default",
		"translations[ru_RU][name]":        sourceProduct["НаименованиеПолное"].(string),
		"translations[ru_RU][description]": sourceProduct["Описание"].(string),
		"translations[ru_RU][slug]":        slug,
	}
	productTaxonsString := strings.Join(productTaxons, ",")
	if len(productTaxonsString) > 0 {
		productData["productTaxons"] = productTaxonsString
	}

	fmt.Println("Get images")
	imagesRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0%D0%9F%D1%80%D0%B8%D1%81%D0%BE%D0%B5%D0%B4%D0%B8%D0%BD%D0%B5%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D1%8B/?$format=json&%24filter=%D0%92%D0%BB%D0%B0%D0%B4%D0%B5%D0%BB%D0%B5%D1%86%D0%A4%D0%B0%D0%B9%D0%BB%D0%B0_Key%20eq%20guid%27"+sourceProductID+"%27", nil)
	images := imagesRaw["value"].([]interface{})
	for i, imageRaw := range images {
		index := strconv.Itoa(i)
		imageRaw := imageRaw.(map[string]interface{})
		imageID := imageRaw["Ref_Key"].(string)
		fmt.Println("- Get image data")
		imageDataRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/InformationRegister_%D0%94%D0%B2%D0%BE%D0%B8%D1%87%D0%BD%D1%8B%D0%B5%D0%94%D0%B0%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D0%BE%D0%B2(%D0%A4%D0%B0%D0%B9%D0%BB='"+imageID+"',%20%D0%A4%D0%B0%D0%B9%D0%BB_Type='StandardODATA.Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0%D0%9F%D1%80%D0%B8%D1%81%D0%BE%D0%B5%D0%B4%D0%B8%D0%BD%D0%B5%D0%BD%D0%BD%D1%8B%D0%B5%D0%A4%D0%B0%D0%B9%D0%BB%D1%8B')/?$format=json", nil)
		base64image := imageDataRaw["ДвоичныеДанныеФайла_Base64Data"].(string)
		base64imageFixed := strings.ReplaceAll(base64image, "\r\n", "")

		reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(base64imageFixed))
		m, formatString, err := image.Decode(reader)
		if err != nil {
			panic(err)
		}
		if formatString != "jpeg" {
			log.Println("Only jpeg image types are supported")
		}

		var imageBuffer bytes.Buffer
		imageWriter := io.Writer(&imageBuffer)

		jpeg.Encode(imageWriter, m, &jpeg.Options{Quality: 85})

		imageType := "default"
		if i == 0 {
			imageType = "main"
		}

		productData["images["+index+"][file]"] = imageBuffer.Bytes()
		productData["images["+index+"][type]"] = imageType
	}

	body, contentType := makeMultipartBody(productData)
	result := syliusRequest("POST", "/api/v1/products/", body, contentType)

	if val, ok := result["errors"]; ok {
		color.Red("ERROR!")
		fmt.Println(productData["productTaxons"])
		fmt.Println(val)
	}
}

func main() {
	fmt.Println("Syncing 1C and Sylius")
	initApp()

	fmt.Println("Get products from 1C")
	productsRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20ne%20%27%27", nil)
	products := productsRaw["value"].([]interface{})
	for _, productRaw := range products {

		sourceProduct := productRaw.(map[string]interface{})
		importProduct(sourceProduct)
	}
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
