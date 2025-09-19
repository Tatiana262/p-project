package kufarfetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"parser-project/internal/core/domain"
	// "parser-project/internal/core/port" // Не нужен здесь, т.к. интерфейс в другом месте
	// "strings"
	"time"

	"github.com/gocolly/colly/v2"
)

// ... (kufarRoot, kufarProps и т.д. остаются такими же) ...
// type kufarRoot struct {
// 	Props kufarProps `json:"props"`
// }

// type kufarProps struct {
// 	InitialState kufarInitialState `json:"initialState"`
// }

// type kufarInitialState struct {
// 	Listings kufarListings `json:"listing"`
// }

type kufarListings struct {
	Ads        []kufarAdItem        `json:"ads"`
	Pagination kufarPages 			`json:"pagination"`
}

type kufarAdItem struct {
	AdId     int `json:"ad_id"`
	AdLink   string `json:"ad_link"`
	ListTime string `json:"list_time"`
}

type kufarPages struct {
	Pages []kufarPaginationItem 	`json:"pages"`
}

type kufarPaginationItem struct {
	Label string      `json:"label"`
	Num   json.Number `json:"num"` 
	Token *string     `json:"token"`
}


func (a *KufarFetcherAdapter) buildURLFromCriteria(criteria domain.Criteria) (string, error) {
    // ... (код без изменений)
	// if criteria.Region == "" || criteria.DealType == "" || criteria.PropertyType == "" {
	// 	return "", fmt.Errorf("kufar adapter: region, dealType, and propertyType are required in criteria")
	// }
	// pathSegments := []string{"l", criteria.Region, criteria.DealType, criteria.PropertyType}
	// if criteria.KufarPathSegment != "" {
	// 	pathSegments = append(pathSegments, criteria.KufarPathSegment)
	// }
	// basePath := "/" + strings.Join(pathSegments, "/")
	queryParams := url.Values{}
	// if criteria.SortBy != "" { 
	// 	queryParams.Set("sort", criteria.SortBy)
	// }
	if criteria.Cursor != "" {
		queryParams.Set("cursor", criteria.Cursor)
	}
	fullURL := a.baseURL
	if len(queryParams) > 0 {
		fullURL += "&" + queryParams.Encode()
	}
	//return "https://api.kufar.by/search-api/v2/search/rendered-paginated?cat=1010&cur=BYR&gtsy=country-belarus~province-brestskaja_oblast~locality-brest&lang=ru&size=200&typ=sell", nil
	return fullURL, nil   // тут можно захардкодить url !!!!!!!!!!!!!1
}

func (a *KufarFetcherAdapter) FetchLinks(ctx context.Context, criteria domain.Criteria, since time.Time) ([]domain.PropertyLink, string, error) {
	// Создаем "одноразовый" клон для этого конкретного запроса
	// Он наследует лимиты, но имеет свои собственные обработчики!
	collector := a.collector.Clone()

	var fetchedLinks []domain.PropertyLink
	var nextCursor string
	
	targetURL, err := a.buildURLFromCriteria(criteria)
	if err != nil {
		return nil, "", fmt.Errorf("kufar adapter: failed to build URL from criteria: %w", err)
	}

	stopProcessing := false

	collector.OnResponse(func(r *colly.Response) {
		if stopProcessing {
			return
		}

		// Десериализуем JSON из тела ответа
		var data kufarListings
		jsonErr := json.Unmarshal(r.Body, &data)
		if jsonErr != nil {
			log.Printf("KufarAdapter: Ошибка при разборе JSON на странице %s: %v\n", r.Request.URL.String(), jsonErr)
			return
		}

		var pageLinks []domain.PropertyLink // Ссылки, собранные с этой страницы
		localStop := false

		if data.Ads != nil {
			for _, ad := range data.Ads {
				listedAt, parseErr := time.Parse(time.RFC3339, ad.ListTime)
				if parseErr != nil {
					log.Printf("KufarAdapter: Ошибка парсинга даты '%s' для URL %s: %v. Пропускаем.\n", ad.ListTime, ad.AdLink, parseErr)
					continue
				}

				if !since.IsZero() && (listedAt.Before(since) || listedAt.Equal(since)) {
					log.Printf("KufarAdapter: Достигнута дата отсечки (%s) для URL %s.\n", since.Format(time.RFC3339), ad.AdLink)
					localStop = true
					break
				}
				pageLinks = append(pageLinks, domain.PropertyLink{URL: ad.AdLink, ListedAt: listedAt, AdID: ad.AdId})
			}
		}
		
		fetchedLinks = append(fetchedLinks, pageLinks...)
		if localStop {
			stopProcessing = true
		}
		
		// Ищем токен пагинации, только если не нужно останавливаться
		if !stopProcessing && data.Pagination.Pages != nil {
			for _, pItem := range data.Pagination.Pages {
				if pItem.Label == "next" && pItem.Token != nil && *pItem.Token != "" {
					nextCursor = *pItem.Token
					break
				}
			}
		}
	})
	
	// Ошибки обрабатываются в глобальном OnError, но мы все равно должны
	// проверить ошибку самого вызова Visit (например, если домен не разрешен)
	visitErr := collector.Visit(targetURL)
	if visitErr != nil {
		return nil, "", fmt.Errorf("kufar adapter: failed to visit URL %s: %w", targetURL, visitErr)
	}
	collector.Wait()

	if stopProcessing {
		log.Println("KufarAdapter: Обработка остановлена из-за достижения 'since' или отмены контекста.")
		nextCursor = "" // Не переходим дальше, если остановились
	}
	
	log.Printf("KufarAdapter: Завершено для URL %s. Ссылок: %d. Следующий курсор: '%s'\n", targetURL, len(fetchedLinks), nextCursor)
	return fetchedLinks, nextCursor, nil
}