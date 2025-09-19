package kufarfetcher

import (
	"log"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
)

// KufarFetcherAdapter отвечает за все взаимодействия с сайтом Kufar.
// Он инкапсулирует в себе настроенный colly.Collector.
type KufarFetcherAdapter struct {
	// Будем хранить один родительский коллектор, который разделяет лимиты
	collector *colly.Collector
	baseURL   string
}

// NewKufarFetcherAdapter - ЕДИНЫЙ конструктор
func NewKufarFetcherAdapter(baseURL string, allowedDomain string, userAgent string, parallelism int, delay time.Duration) *KufarFetcherAdapter {
	// Создаем "родительский" коллектор один раз при инициализации адаптера.
	// c := colly.NewCollector(
	// 	colly.AllowedDomains(allowedDomain),
	// 	colly.UserAgent(userAgent),
	// )

	// // Устанавливаем единые, "вежливые" лимиты для всех запросов к домену
	// // RandomDelay очень важен, чтобы не выглядеть как робот
	// err := c.Limit(&colly.LimitRule{
	// 	DomainGlob:  "*" + allowedDomain,
	// 	Parallelism: parallelism,
	// 	Delay:       delay,
	// 	RandomDelay: 2 * time.Second,
	// })
	// if err != nil {
	// 	// В конструкторе можно паниковать, если базовые настройки неверны
	// 	log.Fatalf("Failed to set limit rule: %v", err)
	// }


	// Создаем "родительский" коллектор один раз при инициализации адаптера.
	c := colly.NewCollector(colly.AllowedDomains("api.kufar.by"))

	// 1. НАСТРАИВАЕМ "ВЕЖЛИВОСТЬ" И МАСКИРОВКУ
	// Эти правила будут наследоваться всеми клонами коллектора.
	err := c.Limit(&colly.LimitRule{
		// Правило будет применяться только к API домену Kufar.
		DomainGlob: "api.kufar.by",

		// Ключевое архитектурное решение:
		// Параллелизм на уровне HTTP-запросов равен 1, потому что
		// реальный параллелизм достигается за счет запуска множества
		// потребителей RabbitMQ. Colly здесь выступает в роли "шлюза",
		// который выстраивает все параллельные вызовы от воркеров
		// в одну последовательную и "вежливую" очередь.
		Parallelism: 1,

		// Вместо статической задержки используем случайную.
		// Это делает поведение парсера менее предсказуемым и более
		// похожим на действия человека. Запрос будет выполнен с задержкой
		// от 0 до 3 секунд после завершения предыдущего.
		RandomDelay: 3 * time.Second,
	})
	if err != nil {
		// Ошибка в базовых настройках - это критично, приложение не сможет работать корректно.
		log.Fatalf("KufarFetcherAdapter: Failed to set limit rule: %v", err)
	}

	// 2. ИСПОЛЬЗУЕМ РАСШИРЕНИЯ ДЛЯ ЛУЧШЕЙ МАСКИРОВКИ
	// Это гораздо эффективнее, чем один статический User-Agent.
	extensions.RandomUserAgent(c) // На каждый запрос будет подставлен User-Agent реального браузера.
	extensions.Referer(c)         // Автоматически подставляет заголовок Referer, имитируя навигацию.


	// 3. УСТАНАВЛИВАЕМ ГЛОБАЛЬНЫЕ ОБРАБОТЧИКИ
	// Они будут полезны для отладки и логирования всех запросов,
	// выполняемых через этот адаптер.
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("KufarFetcherAdapter: Error during request to %s: Status=%d, Error=%v", r.Request.URL, r.StatusCode, err)
	})
	c.OnRequest(func(r *colly.Request) {
		log.Printf("KufarFetcherAdapter: Making request to %s", r.URL.String())
	})

	return &KufarFetcherAdapter{
		collector: c,
		baseURL:   "https://api.kufar.by/search-api/v2/search/rendered-paginated?cat=1010&cur=BYR&gtsy=country-belarus~province-brestskaja_oblast~locality-brest&lang=ru&size=200&typ=sell&rms=v.or%3A3",
	//return "https://api.kufar.by/search-api/v2/search/rendered-paginated?cat=1010&cur=BYR&gtsy=country-belarus~province-brestskaja_oblast~locality-brest&lang=ru&size=200&typ=sell", nil

	}
}