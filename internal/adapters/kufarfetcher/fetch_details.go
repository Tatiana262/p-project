package kufarfetcher

import (
	"context"
	"encoding/json"
	"fmt"
	// "log"
	"regexp"

	// "net/url"
	"parser-project/internal/core/domain"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

// --- kufar JSON parsing structures (as defined before) ---
type kufarAdViewData struct {
	AdID          string          `json:"adId"` // json.Number здесь хорошо, если ID может быть большим
	Subject       string               `json:"subject"`
	Body          string               `json:"body"`
	
	PriceBYN      string               `json:"price"`    // Если это всегда строка в JSON
	PriceUSD      string               `json:"priceUsd"` // Если это всегда строка в JSON
	PricePerSquareMeter string         `json:"pricePerSquareMeter"`
	ListTime      string               `json:"date"`
	Images        kufarImages          `json:"images"`

	// AdParams      map[string]kufarAdParameterItem `json:"adParams"`
	// AccountParams map[string]kufarAccountParameterItem `json:"accountParams"`

	Initial Initial `json:"initial"`

	IsCompanyAd   bool                 `json:"isCompanyAd"`
	Address       string               `json:"address"`
}

type Initial struct {
	AdParams []kufarAdParameterItem `json:"ad_parameters"`
	AccountParams []kufarAccountParameterItem `json:"account_parameters"`
	Currency      string               `json:"currency"`
}

type kufarAdParameterItem struct {
	Label       string            `json:"pl"`
	ValueString interface{}       `json:"vl"` // Используем interface{}
	P           string            `json:"p"`
	V           interface{}       `json:"v"`  // Используем interface{}
	Pu          string            `json:"pu"`
	Order       int               `json:"order"`
	G           []kufarParamGroup `json:"g,omitempty"` // Добавил omitempty, т.к. g не везде есть
}

type kufarAccountParameterItem struct {
	Label string            `json:"pl"`
	Value string            `json:"v"`
	P     string            `json:"p"`
	Pu    string            `json:"pu"`
	Order int               `json:"order"`
	G     []kufarParamGroup `json:"g,omitempty"`
}

type kufarParamGroup struct {
	GroupID    int    `json:"gi"`
	GroupLabel string `json:"gl"`
	GroupOrder int    `json:"go"`
	ParamOrder int    `json:"po"`
}

type kufarImages struct {
	Listings   []string `json:"listings"`
	Gallery    []string `json:"gallery"`
	Thumbnails []string `json:"thumbnails"`
}

type kufarDetailRoot struct {
	Props kufarDetailProps `json:"props"`
}

type kufarDetailProps struct {
	InitialState kufarDetailInitialState `json:"initialState"`
}

type kufarDetailInitialState struct {
	AdView kufarAdViewContainer `json:"adView"`
}

type kufarAdViewContainer struct {
	Data  kufarAdViewData `json:"data"`
	Error string          `json:"error"`
}


// FetchAdDetails извлекает и преобразует детальную информацию об объявлении
func (a *KufarFetcherAdapter) FetchAdDetails(ctx context.Context, adURL string) (*domain.PropertyRecord, error) {
	collector := a.collector.Clone()

	var parsedData kufarAdViewData
	var parseError error
	var wg sync.WaitGroup
	wg.Add(1) // Ожидаем один успешный вызов OnHTML

	collector.OnHTML("script#__NEXT_DATA__", func(e *colly.HTMLElement) {
		defer wg.Done() // Сигнализируем о завершении OnHTML

		jsonDataFromScript := e.Text
		var data kufarDetailRoot
		jsonErr := json.Unmarshal([]byte(jsonDataFromScript), &data)
		if jsonErr != nil {
			parseError = fmt.Errorf("kufar adapter (Detail): failed to unmarshal __NEXT_DATA__ JSON for URL %s: %w", adURL, jsonErr)
			return
		}

		if data.Props.InitialState.AdView.Data.AdID == "" { // Проверка, что данные объявления есть
			parseError = fmt.Errorf("kufar adapter (Detail): ad data not found in __NEXT_DATA__ for URL %s", adURL)
			return
		}
		parsedData = data.Props.InitialState.AdView.Data
	})

	visitErr := collector.Visit(adURL)
	if visitErr != nil {
		return nil, fmt.Errorf("kufar adapter (Detail): failed to visit URL %s: %w", adURL, visitErr)
	}
	collector.Wait() // Ждем завершения HTTP запроса и выполнения OnHTML
	wg.Wait() // Дополнительно ждем wg.Done() из OnHTML

	if parseError != nil {
		return nil, parseError
	}
	if parsedData.AdID == "" { // Если данные так и не были заполнены
		return nil, fmt.Errorf("kufar adapter (Detail): no ad data was parsed from %s", adURL)
	}
	
	// --- Преобразование kufarAdViewData в доменные структуры ---
	record := &domain.PropertyRecord{}
	record.Partner = "kufar"
	record.Source = adURL
	record.Slug = adURL
	record.Title = parsedData.Subject
	record.Address = parsedData.Address
	// record.Slug = slug.Make(parsedData.Subject)
	if parsedData.Body != "" { record.Description = &parsedData.Body }
	
	record.Currency = &parsedData.Initial.Currency
	var totalPrice float64
	if (*record.Currency == "USD") {
		totalPrice = cleanPriceString(parsedData.PriceUSD)
	} else {
		totalPrice = cleanPriceString(parsedData.PriceBYN)
	}

	if parsedData.PricePerSquareMeter != "" {
		pricePerSqMeter := cleanPriceString(parsedData.PricePerSquareMeter)
		record.PricePerSquareMeter = &pricePerSqMeter
	}

	// Даты
	if t, err := time.Parse(time.RFC3339, parsedData.ListTime); err == nil {
		record.PublishedAt = &t
		record.SiteCreatedAt = &t
		record.SiteUpdatedAt = &t
	}
	// Изображения
	if len(parsedData.Images.Gallery) > 0 {
		record.Images = parsedData.Images.Gallery
		record.PreviewImage = &parsedData.Images.Gallery[0]
	}

	// Тип объявления от продавца
	record.AdvertiserType = strPtr("private")
	if parsedData.IsCompanyAd {
		record.AdvertiserType = strPtr("company")
	}
	// Статус по умолчанию
	record.Status = strPtr("active")

	// Контакты
	contactsMap := make(map[string]string)
	for _, accParam := range parsedData.Initial.AccountParams {
		if accParam.P != "address" {
			contactsMap[accParam.Label] = accParam.Value
		}
	}
	if len(contactsMap) > 0 {
		if jsonData, err := json.Marshal(contactsMap); err == nil {
			rawJSON := json.RawMessage(jsonData)
			record.Contacts = &rawJSON
		}
	}
	
	// --- Основной цикл по AdParams ---
	var features, parsedFeatures, roomsNumList []string
	// mappedParamCodes := make(map[string]bool)

	

	

	// Основной цикл
	for _, param := range parsedData.Initial.AdParams {
		// if _, ok := mappedParamCodes[pCode]; ok { continue }

		valStr := valueToString(param.ValueString); if valStr == "" { valStr = valueToString(param.V) }
		if valStr == "" { continue }

		isMapped := true
		switch param.P {
		case "safedeal_enabled", "delivery_enabled", "remuneration_type": // Пропускаем
		case "category": record.Estate = &valStr
		case "region": record.Region = &valStr
		case "area": record.District = &valStr
		case "square_meter": if v, err := strconv.ParseFloat(valStr, 64); err == nil { record.PricePerSquareMeter = &v } // Сохраняем как строку 
		case "size": if v, err := strconv.ParseFloat(valStr, 64); err == nil { record.AreaInSquareMeters = &v }
		case "rooms":
			record.RoomsNumString = &valStr
			// Для rooms_num берем значение из 'v', если оно есть
			if vNumStr := valueToString(param.V); vNumStr != "" {
				roomsNumList = append(roomsNumList, vNumStr)
			}
		case "floor": if v, err := strconv.Atoi(valStr); err == nil { record.FloorNumber = &v }
		case "re_number_floors": if v, err := strconv.Atoi(valStr); err == nil { record.BuildingFloors = &v }
		case "coordinates": 
			if coords, ok := param.V.([]interface{}); ok && len(coords) == 2 {
				// coordMap := map[string]interface{}{"type": "Point", "coordinates": []interface{}{coords[0], coords[1]}}
				if jsonData, err := json.Marshal(coords); err == nil {
					rawJSON := json.RawMessage(jsonData)
					record.Coordinates = &rawJSON
				}
			}

		case "type":
			if valStr == "Продажа" {
				record.TransactionType = strPtr("sell")
				record.TotalPrice = &totalPrice
			} else if valStr == "Аренда" {
				record.TransactionType = strPtr("rent")
				record.RentPrice = &totalPrice
			}


		default:
			isMapped = false
		}

		if isMapped {
			parsedFeatures = append(parsedFeatures, fmt.Sprintf("%s: %s", param.Label, valStr))
		} else {
			features = append(features, fmt.Sprintf("%s: %s", param.Label, valStr))
		}
	}

	if len(features) > 0 { record.Features = features }
	if len(parsedFeatures) > 0 { record.ParsedFeatures = parsedFeatures }
	if len(roomsNumList) > 0 { record.RoomsNum = roomsNumList }

	return record, nil
}


// Вспомогательная функция для получения указателя на строку
func strPtr(s string) *string {
	return &s
}

// Вспомогательная функция для очистки строки с ценой
func cleanPriceString(priceStr string) float64 {
	// Регулярное выражение для удаления всего, кроме цифр и точки/запятой
	re := regexp.MustCompile(`[^\d.,]`)
	cleaned := re.ReplaceAllString(priceStr, "")
	// Заменяем запятую на точку для корректного парсинга
	cleaned = strings.Replace(cleaned, ",", ".", -1)

	price, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0
	}
	return price
}


// Вспомогательная функция для преобразования interface{} в строку
// Вспомогательная функция для преобразования interface{} в строку для вывода
func valueToString(val interface{}) string {
	if val == nil {
		return "(null)"
	}
	switch v := val.(type) {
	case string:
		if v == "" || v == "-" { // Пустую строку или дефис считаем "незначимым" для vl
			return "" // Вернем пустую строку, чтобы потом взять значение из 'V'
		}
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case []interface{}: // Для массивов в vl или v
		var strVals []string
        nonEmptyCount := 0
		for _, item := range v {
            itemStr := valueToString(item) // Рекурсивный вызов для элементов массива
            if itemStr != "" && itemStr != "(null)" { // Собираем только непустые
			    strVals = append(strVals, itemStr)
                nonEmptyCount++
            }
		}
        if nonEmptyCount > 0 {
		    return strings.Join(strVals, ", ") // Не добавляем скобки здесь, т.к. это значение для отображения
        }
        return "" // Если массив пуст или содержит только пустые значения
	default:
		// Для json.Number (если числа приходят так из-за AdID) или других непредвиденных типов
		if num, ok := val.(json.Number); ok {
			return num.String()
		}
		return fmt.Sprintf("%v", v) // Общий случай
	}
}