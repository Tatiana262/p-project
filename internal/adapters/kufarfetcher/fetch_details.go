package kufarfetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"parser-project/internal/core/domain"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

type kufarDetailRoot struct {
	Result kufarAdResult `json:"result"`
}

type kufarAdResult struct {
	AdID          int          `json:"ad_id"` // json.Number здесь хорошо, если ID может быть большим
	AdURL		  string		  `json:"ad_link"`
	Subject       string               `json:"subject"`
	Body          string               `json:"body"`

	PriceBYN      string               `json:"price_byn"`    // Если это всегда строка в JSON
	PriceUSD      string               `json:"price_usd"` // Если это всегда строка в JSON
	Currency      string               `json:"currency"`

	ListTime      string               `json:"list_time"`
	Images        []kufarImage         `json:"images"`

	IsCompanyAd   bool                 `json:"company_ad"`


	AdParams []kufarAdParameterItem `json:"ad_parameters"`
	AccountParams []kufarAccountParameterItem `json:"account_parameters"`
}

type kufarAdParameterItem struct {
	Label       string            `json:"pl"`
	ValueString interface{}       `json:"vl"` // Используем interface{}
	P           string            `json:"p"`
	V           interface{}       `json:"v"`  // Используем interface{}
	Pu          string            `json:"pu"`
	G           []kufarParamGroup `json:"g,omitempty"` // Добавил omitempty, т.к. g не везде есть
}

type kufarAccountParameterItem struct {
	Label string            `json:"pl"`
	Value string            `json:"v"`
	P     string            `json:"p"`
	Pu    string            `json:"pu"`
	G     []kufarParamGroup `json:"g,omitempty"`
}

type kufarParamGroup struct {
	GroupID    int    `json:"gi"`
	GroupLabel string `json:"gl"`
	GroupOrder int    `json:"go"`
	ParamOrder int    `json:"po"`
}

type kufarImage struct {
	Path string `json:"path"`
}


// FetchAdDetails извлекает и преобразует детальную информацию об объявлении
func (a *KufarFetcherAdapter) FetchAdDetails(ctx context.Context, adID int) (*domain.PropertyRecord, error) {
	collector := a.collector.Clone()

	// var parsedData kufarAdViewData
	var adDetails kufarDetailRoot
	var fetchErr error // Для отслеживания ошибок внутри колбэка

	// OnResponse сработает, когда мы получим успешный ответ от API.
	collector.OnResponse(func(r *colly.Response) {
		// Десериализуем JSON из тела ответа
		
		if err := json.Unmarshal(r.Body, &adDetails); err != nil {
			log.Printf("KufarAdapter: Ошибка при разборе JSON деталей для ad_id %d: %v\n", adID, err)
			fetchErr = fmt.Errorf("failed to unmarshal ad details json: %w", err)
			return
		}

	})

	// Формируем URL для API, используя adID
	apiURL := fmt.Sprintf("https://api.kufar.by/search-api/v2/item/%d/rendered", adID)
	visitErr := collector.Visit(apiURL)
	if visitErr != nil {
		return nil, fmt.Errorf("kufar adapter (Detail): failed to visit URL %s: %w", apiURL, visitErr)
	}
	collector.Wait() // Ждем завершения HTTP запроса и выполнения OnHTML

	// Возвращаем ошибку, если она произошла внутри колбэка
    if fetchErr != nil {
        return nil, fetchErr
    }

	
	// --- Преобразование kufarAdViewData в доменные структуры ---
	record := &domain.PropertyRecord{}
	record.Partner = "kufar"
	record.Source = adDetails.Result.AdURL
	record.Slug = adDetails.Result.AdURL
	record.Title = adDetails.Result.Subject
	record.Address = adDetails.Result.Subject

	if adDetails.Result.Body != "" { record.Description = &adDetails.Result.Body }

	var priceBYN float64
	var priceUSD float64

	// 1. Преобразуем цену в BYN
	if priceBynInt, err := strconv.ParseInt(adDetails.Result.PriceBYN, 10, 64); err == nil {
		// err == nil означает, что преобразование в число прошло успешно
		priceBYN = float64(priceBynInt) / 100.0
	} else {
		// Логируем ошибку, если в поле была не цифра, но не останавливаем парсинг
		log.Printf("Could not parse PriceByn '%s' for ad_id %d", adDetails.Result.PriceBYN, adID)
	}

	// 2. Преобразуем цену в USD
	if priceUsdInt, err := strconv.ParseInt(adDetails.Result.PriceUSD, 10, 64); err == nil {
		priceUSD = float64(priceUsdInt) / 100.0
	} else {
		log.Printf("Could not parse PriceUsd '%s' for ad_id %d", adDetails.Result.PriceUSD, adID)
	}
	
	record.Currency = &adDetails.Result.Currency
	var totalPrice float64
	if (*record.Currency == "USD") {
		totalPrice = priceUSD
	} else {
		totalPrice = priceBYN
	}

	// Даты
	if t, err := time.Parse(time.RFC3339, adDetails.Result.ListTime); err == nil {
		record.PublishedAt = &t
		record.SiteCreatedAt = &t
		record.SiteUpdatedAt = &t
	}

	// Изображения
	for i, image := range adDetails.Result.Images {
		if i == 0 {
			record.PreviewImage = &image.Path
		}
		record.Images = append(record.Images, image.Path)
	}

	// Тип объявления от продавца
	record.AdvertiserType = strPtr("private")
	if adDetails.Result.IsCompanyAd {
		record.AdvertiserType = strPtr("company")
	}
	// Статус по умолчанию
	record.Status = strPtr("active")

	// Контакты
	contactsMap := make(map[string]string)
	for _, accParam := range adDetails.Result.AccountParams {
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
	for _, param := range adDetails.Result.AdParams {
		// if _, ok := mappedParamCodes[pCode]; ok { continue }

		valStr := valueToString(param.ValueString); if valStr == "" { valStr = valueToString(param.V) }
		if valStr == "" { continue }

		isMapped := true
		switch param.P {
		case "remuneration_type": // Пропускаем
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