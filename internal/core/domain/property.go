package domain

import "time"

// PropertyLink представляет информацию о ссылке на объект недвижимости
type PropertyLink struct {
	URL      string    `json:"url"`
	ListedAt time.Time `json:"listed_at"`
	Source   string    `json:"source"` // Например, "kufar"
	AdID int `json:"ad_id"`
	// Можно добавить другие поля, если нужно передавать их в очередь,
	// например, ID объявления, если он уже известен на этом этапе
	// AdID string `json:"ad_id"`
}

// Criteria определяет параметры для поиска ссылок на недвижимость
type Criteria struct {
	// Общие фильтры
	Region       string `json:"region"`        // Например, "brestskaya-oblast"
	PropertyType string `json:"property_type"` // "kvartiru", "doma", "kommercheskaya" и т.д. (соответствует частям URL Kufar)
	DealType     string `json:"deal_type"`     // "kupit", "snyat" (соответствует частям URL Kufar)

	// Kufar-специфичные параметры (могут быть и другие)
	KufarPathSegment string `json:"kufar_path_segment,omitempty"` // Например, "5k" или другие сегменты URL для Kufar

	// Сортировка (Kufar по умолчанию по дате, но можем предусмотреть)
	SortBy string `json:"sort_by,omitempty"` // Например, "lst.d" для Kufar

	// Пагинация
	Cursor string `json:"cursor,omitempty"` // Токен для следующей страницы Kufar
}