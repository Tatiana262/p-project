package kufarfetcher

import (
	"log"
	"time"

	"github.com/gocolly/colly/v2"
	// "github.com/gocolly/colly/v2/queue" // Может понадобиться для более сложных сценариев
)

// KufarFetcherAdapter - ЕДИНЫЙ адаптер для работы с kufar.by
type KufarFetcherAdapter struct {
	// Будем хранить один родительский коллектор, который разделяет лимиты
	collector *colly.Collector
	baseURL   string
}

// NewKufarFetcherAdapter - ЕДИНЫЙ конструктор
func NewKufarFetcherAdapter(baseURL string, allowedDomain string, userAgent string, parallelism int, delay time.Duration) *KufarFetcherAdapter {
	// Создаем родительский коллектор ОДИН РАЗ
	c := colly.NewCollector(
		colly.AllowedDomains(allowedDomain),
		colly.UserAgent(userAgent),
	)

	// Устанавливаем единые, "вежливые" лимиты для всех запросов к домену
	// RandomDelay очень важен, чтобы не выглядеть как робот
	err := c.Limit(&colly.LimitRule{
		DomainGlob:  "*" + allowedDomain,
		Parallelism: parallelism,
		Delay:       delay,
		RandomDelay: 2 * time.Second,
	})
	if err != nil {
		// В конструкторе можно паниковать, если базовые настройки неверны
		log.Fatalf("Failed to set limit rule: %v", err)
	}

	// Общие обработчики можно оставить здесь, если они нужны для всех запросов
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("KufarAdapter: Request to %s failed with status %d: %v", r.Request.URL, r.StatusCode, err)
	})
	c.OnRequest(func(r *colly.Request) {
		log.Printf("KufarAdapter: Visiting %s", r.URL.String())
	})

	return &KufarFetcherAdapter{
		collector: c,
		baseURL:   baseURL,
	}
}