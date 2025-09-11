package domain

import (
	"encoding/json"
	"time"
)

// PropertyType определяет тип недвижимости
type PropertyType string

const (
	PropertyTypeApartment    PropertyType = "apartment"
	PropertyTypeHouse        PropertyType = "house"
	PropertyTypeCommercial   PropertyType = "commercial"
	PropertyTypeUnspecified   PropertyType = "unspecified"
)

// DealType определяет тип сделки
type DealType string

const (
	DealTypeSell DealType = "sell"
	DealTypeRent DealType = "rent"
)

type PropertyRecord struct {
	ID                   string           `json:"-" db:"id"`
	CreatedAt            time.Time        `json:"-" db:"created_at"`
	UpdatedAt            *time.Time       `json:"-" db:"updated_at"`
	DeletedAt            *time.Time       `json:"-" db:"deleted_at"`
	IsDuplicated         bool             `json:"-" db:"is_duplicated"`
	CreatedByID          *string          `json:"-" db:"created_by_id"`
	PublishedByID        *string          `json:"-" db:"published_by_id"`
	ResponsibleManagerID *string          `json:"-" db:"responsible_manager_id"`
	
	Partner       string           `json:"partner" db:"partner"`
	Source        string           `json:"source" db:"source"`
	Slug          string           `json:"slug" db:"slug"`
	Title         string           `json:"title" db:"title"`
	Description   *string          `json:"description,omitempty" db:"description"`
	Images        []string         `json:"images,omitempty" db:"images"`
	PreviewImage  *string          `json:"preview_image,omitempty" db:"preview_image"`
	Address       string           `json:"address" db:"address"`
	District      *string          `json:"district,omitempty" db:"district"`
	Region        *string          `json:"region,omitempty" db:"region"`
	Coordinates   *json.RawMessage `json:"coordinates,omitempty" db:"coordinates"`
	Contacts      *json.RawMessage `json:"contacts,omitempty" db:"contacts"`

	TransactionType *string `json:"transaction_type,omitempty" db:"transaction_type"`
	Estate          *string `json:"estate,omitempty" db:"estate"`
	AdvertType      *string `json:"advert_type,omitempty" db:"advert_type"`
	AdvertiserType  *string `json:"advertiser_type,omitempty" db:"advertiser_type"`
	Status          *string `json:"status" db:"status"`

	TotalPrice          *float64 `json:"total_price,omitempty" db:"total_price"`
	RentPrice           *float64 `json:"rent_price,omitempty" db:"rent_price"`
	DepositPrice        *float64 `json:"deposit_price,omitempty" db:"deposit_price"`
	PricePerSquareMeter *float64  `json:"price_per_square_meter,omitempty" db:"price_per_square_meter"` 
	Currency            *string  `json:"currency,omitempty" db:"currency"`
	
	AreaInSquareMeters *float64 `json:"area_in_square_meters,omitempty" db:"area_in_square_meters"`
	RoomsNumString     *string  `json:"rooms_number_string,omitempty" db:"rooms_number_string"`
	RoomsNum           []string `json:"rooms_num,omitempty" db:"rooms_num"`
	FloorNumber        *int     `json:"floor_number,omitempty" db:"floor_number"`
	BuildingFloors     *int     `json:"building_floors,omitempty" db:"building_floors"`
	Note               *string  `json:"note,omitempty" db:"note"`
	Features           []string `json:"features,omitempty" db:"features"`
	ParsedFeatures     []string `json:"parsed_features,omitempty" db:"parsed_features"`
	Marks              []string `json:"marks,omitempty" db:"marks"`

	PublishedAt   *time.Time `json:"published_at,omitempty" db:"published_at"`
	SiteCreatedAt *time.Time `json:"site_created_at" db:"site_created_at"`
	SiteUpdatedAt *time.Time `json:"site_updated_at" db:"site_updated_at"`
}