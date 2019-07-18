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

	"github.com/joho/godotenv"
)

var syliusToken string

func fetchSyliusToken() {
	link := "/api/oauth/v2/token"

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

	syliusToken = decodedBody["access_token"].(string)
}

func initApp() {
	// loads values from .env into the system
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}

	fetchSyliusToken()
}

func syliusRequest(requestType string, url string, body io.Reader, contentType string) map[string]interface{} {
	syliusHost, _ := os.LookupEnv("SYLIUS_HOST")
	req, errRequest := http.NewRequest(requestType, syliusHost+url, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+syliusToken)
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
	sourceProductID := sourceProduct["Ref_Key"].(string)
	slug := sourceProduct["Артикул"].(string)

	fmt.Println("=== Importing product: " + slug + "===")

	// Delete existing product as we can't PUT yet
	// @see: https://github.com/Sylius/Sylius/issues/10532
	syliusRequest("DELETE", "/api/v1/products/"+slug, nil, "application/json")

	productData := map[string]interface{}{
		"code":                      slug,
		"enabled":                   true,
		"channels[0]":               "default",
		"translations[ru_RU][name]": sourceProduct["НаименованиеПолное"].(string),
		"translations[ru_RU][slug]": slug,
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

	// ДополнительныеРеквизиты
	// /1cbooks/odata/standard.odata/ChartOfCharacteristicTypes_%D0%94%D0%BE%D0%BF%D0%BE%D0%BB%D0%BD%D0%B8%D1%82%D0%B5%D0%BB%D1%8C%D0%BD%D1%8B%D0%B5%D0%A0%D0%B5%D0%BA%D0%B2%D0%B8%D0%B7%D0%B8%D1%82%D1%8B%D0%98%D0%A1%D0%B2%D0%B5%D0%B4%D0%B5%D0%BD%D0%B8%D1%8F/?$format=json
	// Parent_Key Группа списка
	// /1cbooks/odata/standard.odata/Catalog_%D0%A1%D0%B5%D0%B3%D0%BC%D0%B5%D0%BD%D1%82%D1%8B%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D1%8B/?$format=json
	// ВидНоменклатуры_Key
	// /1cbooks/odata/standard.odata/Catalog_%D0%92%D0%B8%D0%B4%D1%8B%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D1%8B/?$format=json
	// Производитель_Key
	// /1cbooks/odata/standard.odata/Catalog_%D0%9F%D1%80%D0%BE%D0%B8%D0%B7%D0%B2%D0%BE%D0%B4%D0%B8%D1%82%D0%B5%D0%BB%D0%B8/?$format=json

	body, contentType := makeMultipartBody(productData)
	resp := syliusRequest("POST", "/api/v1/products/", body, contentType)
	fmt.Println(resp)
}

func main() {
	fmt.Println("Syncing 1C and Sylius")
	initApp()

	// /1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?%24format=json&%24filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20ne%20%27%27%20and%20Description%20eq%20%27%D0%97%D0%B2%D1%83%D0%BA%D0%B8%20%D0%B4%D1%83%D1%88%D0%B8.%20%D0%9D.%D0%9D.%20%D0%9D%D0%B5%D0%BF%D0%BB%D1%8E%D0%B5%D0%B2%D0%B0%27
	fmt.Println("Get products from 1C")
	productsRaw := odinCRequest("GET", "/1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json&$filter=%D0%90%D1%80%D1%82%D0%B8%D0%BA%D1%83%D0%BB%20ne%20%27%27", nil)
	products := productsRaw["value"].([]interface{})
	for _, productRaw := range products {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println(r.(error))
			}
		}()
		sourceProduct := productRaw.(map[string]interface{})
		importProduct(sourceProduct)
	}

	//
	// Ref_Key
	// /1cbooks/odata/standard.odata/InformationRegister_ДвоичныеДанныеФайлов(Файл='a58b6122-d46f-11e8-9bf5-14dae924f847', Файл_Type='StandardODATA.Catalog_НоменклатураПрисоединенныеФайлы')/?$format=json
	// ДвоичныеДанныеФайла_Base64Data \r\n

	// /1cbooks/odata/standard.odata/Catalog_%D0%9D%D0%BE%D0%BC%D0%B5%D0%BD%D0%BA%D0%BB%D0%B0%D1%82%D1%83%D1%80%D0%B0/?$format=json
	// /1cbooks/odata/standard.odata/Catalog_НоменклатураПрисоединенныеФайлы/?$format=json
	// /1cbooks/odata/standard.odata/InformationRegister_ДвоичныеДанныеФайлов(Файл='a58b6122-d46f-11e8-9bf5-14dae924f847', Файл_Type='StandardODATA.Catalog_НоменклатураПрисоединенныеФайлы')/?$format=json
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
