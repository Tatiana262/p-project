package constants

import "parser-project/internal/core/domain"

// Deal Types
const (
	DealTypeSell = "kupit"
	DealTypeRent = "snyat"
)

// Property Types
const (
	PropertyTypeApartment  = "kvartiru"
	PropertyTypeHouse      = "dom"
	PropertyTypeCommercial = "kommercheskaya"
)

// Regions
const (
	RegionBrestskayaOblast = "brestskaya-oblast"
	RegionMinskayaOblast   = "minskaya-oblast"
)

// Path Segments (например, для количества комнат)
const (
	Rooms1 = "1k"
	Rooms2 = "2k"
	Rooms3 = "3k"
	Rooms4 = "4k"
	Rooms5 = "5k" 
)

// Sort Options
const (
	SortByDateDesc = "lst.d" // List time descending
)

// Currency Options
const (
    CurrencyUSD = "USD"
    CurrencyBYN = "BYN"
)

// Вы можете также создать структуры для более сложных наборов критериев,
// если планируете запускать парсер для множества предопределенных фильтров.
type PredefinedSearch struct {
    Name         string
    Criteria     domain.Criteria // Используем вашу доменную структуру
}

// GetPredefinedSearches возвращает список предопределенных наборов критериев для парсинга
func GetPredefinedSearches() []PredefinedSearch {
    return []PredefinedSearch{
        {
            Name: "Квартиры_БрестскаяОбласть_Продажа_5комнат",
            Criteria: domain.Criteria{
                Region:           RegionBrestskayaOblast,
                DealType:         DealTypeSell,
                PropertyType:     PropertyTypeApartment,
                // KufarPathSegment: Rooms5,
                SortBy:           SortByDateDesc,
                // Currency: CurrencyUSD, // Можно задавать здесь или глобально
            },
        },
        // {
        //     Name: "Дома_МинскаяОбласть_Аренда",
        //     Criteria: domain.Criteria{
        //         Region:       RegionMinskayaOblast,
        //         DealType:     DealTypeRent,
        //         PropertyType: PropertyTypeHouse,
        //         SortBy:       SortByDateDesc,
        //     },
        // },
    }
}


//return "https://api.kufar.by/search-api/v2/search/rendered-paginated?cat=1010&cur=BYR&gtsy=country-belarus~province-brestskaja_oblast~locality-brest&lang=ru&size=200&typ=sell", nil
