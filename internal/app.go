package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	// "parser-project/internal/adapters/filestorage"
	"parser-project/internal/adapters/kufarfetcher"
	postgres_adapter "parser-project/internal/adapters/postgres"
	rabbitmq_adapter "parser-project/internal/adapters/rabbitmq"
	"parser-project/internal/configs"
	"parser-project/internal/constants"
	"parser-project/internal/core/domain"
	"parser-project/internal/core/port"
	"parser-project/internal/core/usecase"
	"parser-project/pkg/postgres"
	"parser-project/pkg/rabbitmq/rabbitmq_common"
	"parser-project/pkg/rabbitmq/rabbitmq_consumer"
	"parser-project/pkg/rabbitmq/rabbitmq_producer"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	// amqp "github.com/rabbitmq/amqp091-go"
)

// App – структура приложения
type App struct {
	config        *configs.AppConfig
	dbPool        *pgxpool.Pool
	eventProducer *rabbitmq_producer.Publisher

	// Use Case, который запускается самим приложением
	fetchKufarLinksUseCase *usecase.FetchAndEnqueueLinksUseCase

	// Входящие порты (слушатели событий)
	linkEventsListener          port.EventListenerPort
	processedPropEventsListener port.EventListenerPort
}

// NewApp создает новый экземпляр приложения.
// Это "Composition Root", где все зависимости создаются и связываются.
func NewApp() (*App, error) {
	appConfig, err := configs.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("error loading application configuration: %w", err)
	}

	// 1. Инициализация низкоуровневых зависимостей
	dbPool, err := postgres.NewClient(context.Background(), postgres.Config{DatabaseURL: appConfig.Database.URL})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	log.Println("Successfully connected to PostgreSQL pool!")

	producerCfg := rabbitmq_producer.PublisherConfig{
		Config:                   rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
		ExchangeName:             "parser_exchange",
		ExchangeType:             "direct",
		DurableExchange:          true,
		DeclareExchangeIfMissing: true,
	}
	eventProducer, err := rabbitmq_producer.NewPublisher(producerCfg)
	if err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("failed to create event producer: %w", err)
	}
	log.Println("RabbitMQ Event Producer initialized.")

	kufarAdapter := kufarfetcher.NewKufarFetcherAdapter(
		"https://re.kufar.by",
		"re.kufar.by",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/111.0.0.0 Safari/537.36",
		2,               // потоки
		2*time.Second,   // задержка
	)
	log.Println("Unified Kufar Fetcher Adapter initialized.")

	linkQueueAdapter, _ := rabbitmq_adapter.NewRabbitMQLinkQueueAdapter(eventProducer, constants.RoutingKeyLinkTasks)
	processedPropertyQueueAdapter, _ := rabbitmq_adapter.NewRabbitMQProcessedPropertyQueueAdapter(eventProducer, constants.RoutingKeyProcessedProperties)
	pgLastRunRepo, _ := postgres_adapter.NewPostgresLastRunRepository(dbPool)

	//fileStorageAdapter, _ := filestorage.NewPropertyFileStorageAdapter("processed_properties_pretty.json")

	postgresStorageAdapter, err := postgres_adapter.NewPostgresStorageAdapter(dbPool) // <-- Передаем dbPool!
    if err != nil {
        eventProducer.Close() 
		dbPool.Close()       
        return nil, fmt.Errorf("failed to create postgres storage adapter: %w", err)
    }
    
	log.Println("All outgoing adapters initialized.")

	// 3. ИНИЦИАЛИЗАЦИЯ USE CASES (ядра бизнес-логики)
	fetchKufarUseCase := usecase.NewFetchAndEnqueueLinksUseCase(kufarAdapter, linkQueueAdapter, pgLastRunRepo, "kufar")
	processLinkUseCase := usecase.NewProcessLinkUseCase(kufarAdapter, processedPropertyQueueAdapter)
	savePropertyUseCase := usecase.NewSavePropertyUseCase(postgresStorageAdapter)
	log.Println("All use cases initialized.")

	// 4. ИНИЦИАЛИЗАЦИЯ ВХОДЯЩИХ АДАПТЕРОВ (те, которые ВЫЗЫВАЮТ наше ядро)
	linksConsumerCfg := rabbitmq_consumer.ConsumerConfig{
		Config:              rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
		QueueName:           constants.QueueLinkTasks,
		RoutingKeyForBind:   constants.RoutingKeyLinkTasks,
		ExchangeNameForBind: "parser_exchange",
		PrefetchCount:       5,
		DurableQueue:        true,
		ConsumerTag:         "link-processor-adapter",
		DeclareQueue:        true,
	}
	linkListener, err := rabbitmq_adapter.NewLinkConsumerAdapter(linksConsumerCfg, processLinkUseCase)
	if err != nil {
		eventProducer.Close()
		dbPool.Close()
		return nil, err
	}
	log.Println("Link Events Listener initialized.")

	processedConsumerCfg := rabbitmq_consumer.ConsumerConfig{
		Config:              rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
		QueueName:           constants.QueueProcessedProperties,
		DurableQueue:        true,
		ExchangeNameForBind: "parser_exchange",
		RoutingKeyForBind:   constants.RoutingKeyProcessedProperties,
		PrefetchCount:       1,
		ConsumerTag:         "property-saver-adapter",
		DeclareQueue:        true,
	}
	processedPropListener, err := rabbitmq_adapter.NewProcessedPropertyConsumerAdapter(processedConsumerCfg, savePropertyUseCase)
	if err != nil {
		linkListener.Close()
		eventProducer.Close()
		dbPool.Close()
		return nil, err
	}
	log.Println("Processed Property Events Listener initialized.")

	// 5. Собираем приложение
	application := &App{
		config:                      appConfig,
		dbPool:                      dbPool,
		eventProducer:               eventProducer,
		fetchKufarLinksUseCase:      fetchKufarUseCase, // Нужен для прямого вызова
		linkEventsListener:          linkListener,
		processedPropEventsListener: processedPropListener,
	}

	return application, nil
}

// StartKufarLinkFetcher запускает процесс сбора ссылок.
func (a *App) StartKufarLinkFetcher(ctx context.Context) {
	log.Println("App: Initiating Kufar link fetching...")
	searches := constants.GetPredefinedSearches()

	for _, search := range searches {
		go func(crit domain.Criteria, searchName string) {
			if err := a.fetchKufarLinksUseCase.Execute(ctx, crit); err != nil {
				log.Printf("App: Kufar link fetching for '%s' finished with error: %v", searchName, err)
			} else {
				log.Printf("App: Kufar link fetching for '%s' finished successfully.", searchName)
			}
		}(search.Criteria, search.Name)
	}
}

// Run запускает все компоненты приложения и управляет их жизненным циклом.
func (a *App) Run() error {
	// Создаем единый контекст для всего приложения для управления graceful shutdown
	appCtx, cancelApp := context.WithCancel(context.Background())
	//defer cancelApp()

	// Используем WaitGroup для ожидания завершения всех фоновых задач
	var wg sync.WaitGroup

	defer func() {
		log.Println("App: Shutdown sequence initiated...")

		// Ждем завершения всех запущенных горутин (слушателей)
		log.Println("App: Waiting for background processes to finish...")
		wg.Wait()
		log.Println("App: All background processes finished.")

		// Теперь безопасно закрываем ресурсы
		if a.linkEventsListener != nil {
			if err := a.linkEventsListener.Close(); err != nil {
				log.Printf("App: Error closing links listener: %v\n", err)
			}
		}
		if a.processedPropEventsListener != nil {
			if err := a.processedPropEventsListener.Close(); err != nil {
				log.Printf("App: Error closing processed properties listener: %v\n", err)
			}
		}
		if a.eventProducer != nil {
			if err := a.eventProducer.Close(); err != nil {
				log.Printf("App: Error closing event producer: %v\n", err)
			}
		}
		if a.dbPool != nil {
			a.dbPool.Close()
			log.Println("App: PostgreSQL pool closed.")
		}
		log.Println("Application shut down gracefully.")
	}()

	log.Println("Application is starting...")

	// Запускаем сборщик ссылок, передавая ему главный контекст
	a.StartKufarLinkFetcher(appCtx)

	consumerErrors := make(chan error, 2)

	// Функция-хелпер для запуска слушателей
	startListener := func(name string, listener port.EventListenerPort) {
		defer wg.Done()
		log.Printf("App: Starting %s...", name)
		if err := listener.Start(appCtx); err != nil {
			log.Printf("App: %s stopped with an unexpected error: %v", name, err)
			consumerErrors <- fmt.Errorf("%s error: %w", name, err)
		} else {
			log.Printf("App: %s stopped gracefully due to context cancellation.", name)
		}
	}

	wg.Add(2)
	go startListener("Links Events Listener", a.linkEventsListener)
	go startListener("Processed Property Events Listener", a.processedPropEventsListener)

	// Ожидание сигнала на завершение или ошибки от одного из компонентов
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Application running. Waiting for signals or consumer error...")
	select {
	case receivedSignal := <-quit:
		log.Printf("App: Received signal: %s. Shutting down...\n", receivedSignal)
	case err := <-consumerErrors:
		log.Printf("App: A critical component failed: %v. Shutting down...\n", err)
	case <-appCtx.Done():
		log.Println("App: Context was cancelled unexpectedly. Shutting down...")
	}

	// Инициируем graceful shutdown, отменяя главный контекст
	cancelApp()

	return nil
}












// package internal

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"os"
// 	"os/signal"
// 	"parser-project/internal/adapters/filestorage" // <-- Новый импорт
// 	"parser-project/internal/adapters/kufarfetcher"
// 	postgres_adapter "parser-project/internal/adapters/postgres"
// 	rabbitmq_adapter "parser-project/internal/adapters/rabbitmq"
// 	"parser-project/internal/configs"
// 	"parser-project/internal/constants"
// 	"parser-project/internal/core/domain"
// 	"parser-project/internal/core/usecase"
// 	"parser-project/pkg/postgres"
// 	"parser-project/pkg/rabbitmq/rabbitmq_common"
// 	"parser-project/pkg/rabbitmq/rabbitmq_consumer"
// 	"parser-project/pkg/rabbitmq/rabbitmq_producer"
// 	"syscall"
// 	"time"

// 	"github.com/jackc/pgx/v5/pgxpool"
// 	amqp "github.com/rabbitmq/amqp091-go"
// )

// // App – структура приложения
// type App struct {
// 	config *configs.AppConfig
// 	dbPool *pgxpool.Pool

// 	// RabbitMQ компоненты
// 	eventProducer             *rabbitmq_producer.Publisher
// 	linksTaskConsumer         *rabbitmq_consumer.Consumer
// 	processedPropertyConsumer *rabbitmq_consumer.Consumer

// 	// --- Use Cases (Сценарии использования) ---
// 	fetchKufarLinksUseCase *usecase.FetchAndEnqueueLinksUseCase
// 	processLinkUseCase     *usecase.ProcessLinkUseCase     // <-- Новый Use Case
// 	savePropertyUseCase    *usecase.SavePropertyUseCase    // <-- Новый Use Case
// }

// // NewApp создает новый экземпляр приложения
// func NewApp() (*App, error) {
// 	appConfig, err := configs.LoadConfig()
// 	if err != nil {
// 		return nil, fmt.Errorf("error loading application configuration: %w", err)
// 	}

// 	// 1. Инициализация PostgreSQL
// 	dbPool, err := postgres.NewClient(context.Background(), postgres.Config{DatabaseURL: appConfig.Database.URL})
// 	if err != nil { return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err) }
// 	log.Println("Successfully connected to PostgreSQL pool!")

// 	// 2. Инициализация RabbitMQ Продюсера
// 	producerCfg := rabbitmq_producer.PublisherConfig{
// 		Config:                   rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
// 		ExchangeName:             "parser_exchange",
// 		ExchangeType:             "direct",
// 		DurableExchange:          true,
// 		DeclareExchangeIfMissing: true,
// 	}
// 	eventProducer, err := rabbitmq_producer.NewPublisher(producerCfg)
// 	if err != nil {
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create event producer: %w", err)
// 	}
// 	log.Println("RabbitMQ Event Producer initialized.")

// 	// 3. --- Инициализация АДАПТЕРОВ (реализаций портов) ---
	
// 	// 3.1 Адаптер для получения данных с Kufar
// 	kufarFetcherAdapter := kufarfetcher.NewKufarFetcherLinksAdapter(
// 		"https://re.kufar.by", "re.kufar.by",
// 		"Mozilla/5.0 (...", 1, 2*time.Second,
// 	)
// 	log.Println("Kufar Fetcher Adapter initialized.")

// 	// 3.2 Адаптеры для очередей RabbitMQ
// 	linkQueueAdapter, _ := rabbitmq_adapter.NewRabbitMQLinkQueueAdapter(eventProducer, constants.RoutingKeyLinkTasks)
// 	log.Println("Link Queue Adapter initialized.")
	
// 	processedPropertyQueueAdapter, _ := rabbitmq_adapter.NewRabbitMQProcessedPropertyQueueAdapter(eventProducer, constants.RoutingKeyProcessedProperties)
// 	log.Println("Processed Property Queue Adapter initialized.")

// 	// 3.3 Адаптер для репозитория в Postgres
// 	pgLastRunRepo, _ := postgres_adapter.NewPostgresLastRunRepository(dbPool)
// 	log.Println("Postgres Last Run Repository initialized.")

// 	// 3.4. Адаптер для сохранения в файл <-- НОВЫЙ АДАПТЕР
// 	fileStorageAdapter, err := filestorage.NewPropertyFileStorageAdapter("processed_properties_pretty.json")
// 	if err != nil {
// 		eventProducer.Close()
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create file storage adapter: %w", err)
// 	}
// 	log.Println("File Storage Adapter initialized.")


// 	// 4. --- Инициализация USE CASES (ядра бизнес-логики) ---
	
// 	fetchKufarUseCase := usecase.NewFetchAndEnqueueLinksUseCase(
// 		kufarFetcherAdapter,
// 		linkQueueAdapter,
// 		pgLastRunRepo,
// 		"kufar",
// 	)
// 	log.Println("Fetch Kufar Links Use Case initialized.")

// 	// <-- НОВЫЙ USE CASE для обработки ссылок
// 	processLinkUseCase := usecase.NewProcessLinkUseCase(
// 		kufarFetcherAdapter,             // Реализация порта PropertyDetailsFetcherPort
// 		processedPropertyQueueAdapter, // Реализация порта ProcessedPropertyQueuePort
// 	)
// 	log.Println("Process Link Use Case initialized.")

// 	// <-- НОВЫЙ USE CASE для сохранения данных
// 	savePropertyUseCase := usecase.NewSavePropertyUseCase(
// 		fileStorageAdapter, // Реализация порта PropertyStoragePort
// 	)
// 	log.Println("Save Property Use Case initialized.")
	

// 	// 5. Собираем приложение
// 	application := &App{
// 		config:                 appConfig,
// 		dbPool:                 dbPool,
// 		eventProducer:          eventProducer,
// 		fetchKufarLinksUseCase: fetchKufarUseCase,
// 		processLinkUseCase:     processLinkUseCase,
// 		savePropertyUseCase:    savePropertyUseCase,
// 	}

// 	// 6. --- Инициализация Консьюмеров (точек входа для асинхронной логики) ---
	
// 	// 6.1 Консьюмер для ПЕРВОЙ очереди (обработка ссылок)
// 	linksConsumerCfg := rabbitmq_consumer.ConsumerConfig{
// 		Config: rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
// 		QueueName: constants.QueueLinkTasks,
// 		RoutingKeyForBind: constants.RoutingKeyLinkTasks,
// 		ExchangeNameForBind: "parser_exchange",
// 		PrefetchCount: 5,
// 		DurableQueue: true,
// 		ConsumerTag: "link-processor",
// 	}
// 	// Передаем новый, тонкий обработчик
// 	linksTaskConsumer, err := rabbitmq_consumer.NewConsumer(linksConsumerCfg, application.messageHandlerForLinkProcessing)
// 	if err != nil { /*... cleanup ...*/ return nil, err }
// 	application.linksTaskConsumer = linksTaskConsumer
// 	log.Println("Links Processing Task Consumer initialized.")

// 	// 6.2 Консьюмер для ВТОРОЙ очереди (сохранение в файл)
// 	processedConsumerCfg := rabbitmq_consumer.ConsumerConfig{
// 		Config: rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
// 		QueueName: constants.QueueProcessedProperties,
// 		DurableQueue: true,
// 		ExchangeNameForBind: "parser_exchange",
// 		RoutingKeyForBind: constants.RoutingKeyProcessedProperties,
// 		PrefetchCount: 1,
// 		ConsumerTag: "property-saver-to-file",
// 	}
// 	// Передаем новый, тонкий обработчик
// 	processedConsumer, err := rabbitmq_consumer.NewConsumer(processedConsumerCfg, application.messageHandlerForSavingToFile)
// 	if err != nil { /*... cleanup ...*/ return nil, err }
// 	application.processedPropertyConsumer = processedConsumer
// 	log.Println("Processed Property to File Consumer initialized.")


// 	return application, nil
// }


// // messageHandlerForLinkProcessing - ТЕПЕРЬ это тонкая обертка, которая вызывает UseCase.
// func (a *App) messageHandlerForLinkProcessing(d amqp.Delivery) (ack bool, requeueOnError bool, err error) {
// 	log.Printf("Link Processor Handler: Received task (Tag: %d)\n", d.DeliveryTag)

// 	var linkToParse domain.PropertyLink
// 	if err := json.Unmarshal(d.Body, &linkToParse); err != nil {
// 		log.Printf("Link Processor Handler: Error unmarshalling link data: %v. Message will be NACKed (no requeue).\n", err)
// 		return false, false, fmt.Errorf("unmarshal error: %w", err) // Не повторять, "битое" сообщение
// 	}

// 	// Вся сложная логика теперь здесь:
// 	err = a.processLinkUseCase.Execute(context.Background(), linkToParse)
// 	if err != nil {
// 		log.Printf("Link Processor Handler: Use case execution failed: %v. Requeueing task.\n", err)
// 		return false, true, err // Ошибка при выполнении, нужно повторить (например, сайт был недоступен)
// 	}
	
// 	log.Printf("Link Processor Handler: Task successfully processed (Tag: %d). Acknowledging.\n", d.DeliveryTag)
// 	return true, false, nil // Все прошло успешно, подтверждаем сообщение
// }


// // messageHandlerForSavingToFile - ТЕПЕРЬ это тонкая обертка, которая вызывает UseCase.
// func (a *App) messageHandlerForSavingToFile(d amqp.Delivery) (ack bool, requeueOnError bool, err error) {
// 	log.Printf("File Saver Handler: Received data (Tag: %d)\n", d.DeliveryTag)

// 	var propertyRecord domain.PropertyRecord
// 	if err := json.Unmarshal(d.Body, &propertyRecord); err != nil {
// 		log.Printf("File Saver Handler: Error unmarshalling property record data: %v. Message will be NACKed (no requeue).\n", err)
// 		return false, false, fmt.Errorf("unmarshal error: %w", err)
// 	}

// 	// Логика сохранения теперь инкапсулирована в use case:
// 	err = a.savePropertyUseCase.Execute(context.Background(), propertyRecord)
// 	if err != nil {
// 		log.Printf("File Saver Handler: Use case execution failed: %v. Requeueing message.\n", err)
// 		return false, true, err // Ошибка при сохранении (например, нет прав на запись), повторяем
// 	}

// 	log.Printf("File Saver Handler: Record successfully saved (Tag: %d). Acknowledging.\n", d.DeliveryTag)
// 	return true, false, nil // Успех, подтверждаем
// }

// // StartKufarLinkFetcher запускает процесс сбора ссылок с Kufar для определенных критериев.
// // Эту функцию можно вызывать по расписанию или по триггеру.
// func (a *App) StartKufarLinkFetcher(ctx context.Context) {
// 	log.Println("App: Initiating Kufar link fetching...")

// 	searches := constants.GetPredefinedSearches()

// 	for _, search := range searches {
//         log.Printf("App: Starting fetcher for: %s\n", search.Name)

//         go func(crit domain.Criteria, searchName string) {
//             err := a.fetchKufarLinksUseCase.Execute(ctx, crit)
//             if err != nil {
//                 log.Printf("App: Kufar link fetching process for '%s' finished with error: %v", searchName, err)
//             } else {
//                 log.Printf("App: Kufar link fetching process for '%s' finished successfully.", searchName)
//             }
//         }(search.Criteria, search.Name) // Передаем копию критериев и имя
//     }
// }

// // ... (StartKufarLinkFetcher и Run остаются почти такими же, но Run должен закрывать eventProducer) ...
// func (a *App) Run() error {
//     defer func() {
// 		log.Println("App: Initiating shutdown sequence...")

// 		if a.linksTaskConsumer != nil {
// 			log.Println("App: Closing Links Task Consumer...")
// 			if err := a.linksTaskConsumer.Close(); err != nil {
// 				log.Printf("App: Error closing links task consumer: %v\n", err)
// 			}
// 		}

// 		if a.processedPropertyConsumer != nil {
// 			log.Println("App: Closing Processed Property Consumer...")
// 			if err := a.processedPropertyConsumer.Close(); err != nil {
// 				log.Printf("App: Error closing processed property consumer: %v\n", err)
// 			}
// 		}
		
// 		if a.eventProducer != nil {
// 			log.Println("App: Closing Event Producer...")
// 			if err := a.eventProducer.Close(); err != nil {
// 				log.Printf("App: Error closing event producer: %v\n", err)
// 			}
// 		}
		
// 		if a.dbPool != nil {
// 			log.Println("App: Closing PostgreSQL pool...")
// 			a.dbPool.Close()
// 		}
// 		log.Println("Application shut down gracefully.")
// 	}()
		
// 	log.Println("Application is starting...")

// 	// Запускаем сборщик ссылок Kufar
// 	// Используем context.Background() для простоты, в реальном приложении
// 	// может быть контекст с возможностью отмены извне.
// 	go a.StartKufarLinkFetcher(context.Background()) // Запускаем как горутину

// 	// Запускаем консьюмера задач для обработки ссылок
// 	consumerErrors := make(chan error, 2)
// 	// Запускаем консьюмера ПЕРВОЙ очереди (парсинг деталей)
// 	go func() {
// 		log.Println("App: Starting Links Processing Task Consumer...")
// 		appCtx, appCancel := context.WithCancel(context.Background())
// 		defer appCancel() // Гарантированный вызов cancel при выходе из Run
// 		err := a.linksTaskConsumer.StartConsuming(appCtx)
		
// 		amqpErr, ok := err.(*amqp.Error)
//         if err != nil && (!ok || amqpErr.Code != amqp.ConnectionForced) {
// 			log.Printf("App: Links Processing Task Consumer stopped with an unexpected error: %v", err)
// 			select {
// 			case consumerErrors <- fmt.Errorf("links task consumer error: %w", err):
// 			default:
// 			}
// 		} else {
// 			log.Println("App: Links Processing Task Consumer stopped gracefully.")
// 		}
// 	}()

// 	// --- НОВЫЙ БЛОК: Запускаем консьюмера ВТОРОЙ очереди (сохранение в файл) ---
// 	go func() {
// 		log.Println("App: Starting Processed Property Saver Consumer...")
// 		appCtx, appCancel := context.WithCancel(context.Background())
// 		defer appCancel() // Гарантированный вызов cancel при выходе из Run
// 		err := a.processedPropertyConsumer.StartConsuming(appCtx)

// 		amqpErr, ok := err.(*amqp.Error)
//         if err != nil && (!ok || amqpErr.Code != amqp.ConnectionForced) {
// 			log.Printf("App: Processed Property Saver Consumer stopped with an unexpected error: %v", err)
// 			select {
// 			case consumerErrors <- fmt.Errorf("property saver consumer error: %w", err):
// 			default:
// 			}
// 		} else {
// 			log.Println("App: Processed Property Saver Consumer stopped gracefully.")
// 		}
// 	}()

// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

// 	log.Println("Application running. Waiting for signals or consumer error...")
// 	select {
// 	case receivedSignal := <-quit:
// 		log.Printf("App: Received signal: %s. Shutting down...\n", receivedSignal)
// 	case err := <-consumerErrors:
// 		log.Printf("App: A consumer stopped due to an unexpected error: %v. Shutting down...\n", err)
// 	}

// 	return nil
// }






// package internal

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"os"
// 	"os/signal"
// 	"parser-project/internal/adapters/kufarfetcher"              // Адаптер для Kufar
// 	postgres_adapter "parser-project/internal/adapters/postgres" // Адаптер для PostgreSQL
// 	rabbitmq_adapter "parser-project/internal/adapters/rabbitmq" // Адаптер для RabbitMQ
// 	"parser-project/internal/configs"
// 	"parser-project/internal/constants"
// 	"parser-project/internal/core/domain"
// 	"parser-project/internal/core/usecase"
// 	"parser-project/pkg/postgres"
// 	"parser-project/pkg/rabbitmq/rabbitmq_common"
// 	"parser-project/pkg/rabbitmq/rabbitmq_consumer"
// 	"parser-project/pkg/rabbitmq/rabbitmq_producer"
// 	"syscall"
// 	"time"

// 	"github.com/jackc/pgx/v5/pgxpool"
// 	amqp "github.com/rabbitmq/amqp091-go"
// )

// // App – это структура, которая инкапсулирует все приложение
// type App struct {
// 	config             *configs.AppConfig
// 	dbPool             *pgxpool.Pool
// 	linksEventProducer *rabbitmq_producer.Publisher // Оставляем для общих нужд или других продюсеров
// 	linksTaskConsumer  *rabbitmq_consumer.Consumer

// 	// Новые компоненты для гексагональной архитектуры
// 	kufarFetcherSvc             *kufarfetcher.KufarFetcherAdapter
// 	rabbitMQLinkQueueSvc        *rabbitmq_adapter.RabbitMQLinkQueueAdapter
// 	postgresLastRunRepoSvc      *postgres_adapter.PostgresLastRunRepository
// 	fetchKufarLinksUseCase *usecase.FetchAndEnqueueLinksUseCase
// }

// // NewApp создает новый экземпляр приложения
// func NewApp() (*App, error) {
// 	appConfig, err := configs.LoadConfig()
// 	if err != nil {
// 		return nil, fmt.Errorf("error loading application configuration: %w", err)
// 	}

// 	// 1. Инициализация PostgreSQL
// 	pgConfig := postgres.Config{
// 		DatabaseURL: appConfig.Database.URL,
// 	}
// 	dbPool, err := postgres.NewClient(context.Background(), pgConfig)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
// 	}
// 	log.Println("Successfully connected to PostgreSQL pool!")

// 	// 2. Инициализация RabbitMQ Продюсера (может использоваться для разных целей)
// 	linksProducerCfg := rabbitmq_producer.PublisherConfig{
// 		Config:                   rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
// 		ExchangeName:             "parser_exchange", // Общий обменник
// 		ExchangeType:             "direct",
// 		DurableExchange:          true,
// 		DeclareExchangeIfMissing: true,
// 	}
// 	linksEventProducer, err := rabbitmq_producer.NewPublisher(linksProducerCfg)
// 	if err != nil {
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create links event producer: %w", err)
// 	}
// 	log.Println("Generic Event Producer initialized.")

// 	// 3. Инициализация адаптеров
// 	// 3.1 Kufar Fetcher Adapter
// 	kufarFetcher := kufarfetcher.NewKufarFetcherAdapter(
// 		"https://re.kufar.by",               // baseURL
// 		"re.kufar.by",                       // allowedDomain
// 		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/111.0.0.0 Safari/537.36", // userAgent
// 		1,                                   // parallelism
// 		2*time.Second,                       // delay
// 	)
// 	log.Println("Kufar Fetcher Adapter initialized.")

// 	// 3.2 RabbitMQ Link Queue Adapter
// 	// Используем ранее созданный linksEventProducer, так как он уже подключен к RabbitMQ
// 	// и настроен на нужный exchange. Меняем только routingKey.
// 	kufarLinksQueue, err := rabbitmq_adapter.NewRabbitMQLinkQueueAdapter(linksEventProducer, "kufar.links.tasks")
// 	if err != nil {
// 		linksEventProducer.Close()
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create Kufar links queue adapter: %w", err)
// 	}
// 	log.Println("Kufar Links Queue Adapter initialized.")

// 	// 3.3 Postgres Last Run Repository Adapter
// 	pgLastRunRepo, err := postgres_adapter.NewPostgresLastRunRepository(dbPool)
// 	if err != nil {
// 		linksEventProducer.Close()
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create Postgres last run repository: %w", err)
// 	}
// 	log.Println("Postgres Last Run Repository initialized.")

// 	// 4. Инициализация Use Case
// 	fetchKufarUseCase := usecase.NewFetchAndEnqueueLinksUseCase(
// 		kufarFetcher,
// 		kufarLinksQueue,
// 		pgLastRunRepo,
// 		"kufar", // sourceName
// 	)
// 	log.Println("Fetch Kufar Links Use Case initialized.")

// 	// Собираем приложение
// 	application := &App{
// 		config:                   appConfig,
// 		dbPool:                   dbPool,
// 		linksEventProducer:       linksEventProducer, // Может понадобиться для других задач
// 		kufarFetcherSvc:          kufarFetcher,
// 		rabbitMQLinkQueueSvc:     kufarLinksQueue,
// 		postgresLastRunRepoSvc:   pgLastRunRepo,
// 		fetchKufarLinksUseCase: fetchKufarUseCase,
// 	}

// 	// 5. Инициализация RabbitMQ Консьюмера (для обработки ссылок, если нужно в этом же приложении)
// 	// Эту часть можно будет вынести в отдельный сервис/воркер,
// 	// который будет слушать очередь "link_parsing_tasks"
// 	linksConsumerCfg := rabbitmq_consumer.ConsumerConfig{
// 		Config:                 rabbitmq_common.Config{URL: appConfig.RabbitMQ.URL},
// 		QueueName:              "link_parsing_tasks", // Очередь, куда KufarLinksQueueAdapter будет класть задачи
// 		DeclareQueue:           true,
// 		DurableQueue:           true,
// 		ExchangeNameForBind:    "parser_exchange", // Тот же обменник, что и у продюсера
// 		DeclareExchangeForBind: false,             // Обменник уже объявлен продюсером
// 		ExchangeTypeForBind:    "direct",
// 		RoutingKeyForBind:      "kufar.links.tasks", // Ключ, по которому приходят сообщения от адаптера
// 		PrefetchCount:          1,
// 		ConsumerTag:            "link-processor", // Другой тег для консьюмера, обрабатывающего ссылки
// 		ExclusiveConsumer:      false,
// 	}

// 	// messageHandlerForLinks будет обрабатывать фактический парсинг страницы по ссылке
// 	linksTaskConsumer, err := rabbitmq_consumer.NewConsumer(linksConsumerCfg, application.messageHandlerForLinkProcessing)
// 	if err != nil {
// 		// ... корректное закрытие всех инициализированных ресурсов ...
// 		linksEventProducer.Close()
// 		dbPool.Close()
// 		return nil, fmt.Errorf("failed to create links task consumer: %w", err)
// 	}
// 	application.linksTaskConsumer = linksTaskConsumer
// 	log.Println("Links Processing Task Consumer initialized.")

// 	return application, nil
// }

// // messageHandlerForLinkProcessing - это обработчик для сообщений из очереди ссылок.
// // Здесь должна быть логика парсинга конкретной страницы объявления.
// func (a *App) messageHandlerForLinkProcessing(d amqp.Delivery) (ack bool, requeueOnError bool, err error) {
// 	log.Printf("Link Processor: Received link task: BodyLen %d (Tag: %d)\n", len(d.Body), d.DeliveryTag)

// 	var linkToParse domain.PropertyLink
// 	if err := json.Unmarshal(d.Body, &linkToParse); err != nil {
// 		log.Printf("Link Processor: Error unmarshalling link data: %v. Message will be NACKed (no requeue).\n", err)
// 		return false, false, fmt.Errorf("failed to unmarshal link data: %w", err) // Не перепосылать, если структура неверна
// 	}

// 	log.Printf("Link Processor: Processing URL: %s (ListedAt: %s, Source: %s)\n", linkToParse.URL, linkToParse.ListedAt, linkToParse.Source)

// 	// TODO: Здесь должна быть логика вызова вашего парсера для linkToParse.URL
// 	// Например:
// 	// propertyDetails, err := a.someDetailParserService.ParseDetails(linkToParse.URL)
// 	// if err != nil {
// 	//     log.Printf("Link Processor: Error parsing details for %s: %v. Requeue: true (или false, в зависимости от типа ошибки)\n", linkToParse.URL, err)
// 	//     return false, true, err // Пример: перепослать при временной ошибке сети
// 	// }
// 	// log.Printf("Link Processor: Successfully parsed details for %s: %+v\n", linkToParse.URL, propertyDetails)
// 	// // TODO: Сохранить propertyDetails в базу данных

// 	time.Sleep(500 * time.Millisecond) // Имитация обработки

// 	// Пример тестовой логики из вашего предыдущего messageHandler
// 	if linkToParse.URL == "http://example.com/fail_requeue" {
// 		log.Printf("Link Processor: Simulating error for '%s', requesting requeue.\n", linkToParse.URL)
// 		return false, true, fmt.Errorf("simulated processing error, requeue this")
// 	}
// 	if linkToParse.URL == "http://example.com/fail_no_requeue" {
// 		log.Printf("Link Processor: Simulating fatal error for '%s', NO requeue.\n", linkToParse.URL)
// 		return false, false, fmt.Errorf("simulated fatal error, do not requeue this")
// 	}

// 	log.Printf("Link Processor: Successfully processed (simulated) '%s'.\n", linkToParse.URL)
// 	return true, false, nil
// }


// // StartKufarLinkFetcher запускает процесс сбора ссылок с Kufar для определенных критериев.
// // Эту функцию можно вызывать по расписанию или по триггеру.
// func (a *App) StartKufarLinkFetcher(ctx context.Context) {
// 	log.Println("App: Initiating Kufar link fetching...")

// 	searches := constants.GetPredefinedSearches()

// 	for _, search := range searches {
//         log.Printf("App: Starting fetcher for: %s\n", search.Name)

//         go func(crit domain.Criteria, searchName string) {
//             err := a.fetchKufarLinksUseCase.Execute(ctx, crit)
//             if err != nil {
//                 log.Printf("App: Kufar link fetching process for '%s' finished with error: %v", searchName, err)
//             } else {
//                 log.Printf("App: Kufar link fetching process for '%s' finished successfully.", searchName)
//             }
//         }(search.Criteria, search.Name) // Передаем копию критериев и имя
//     }

	
// }


// // Run запускает все компоненты приложения и блокирует до сигнала завершения
// func (a *App) Run() error {
// 	// ... (ваш defer для закрытия ресурсов остается почти таким же, нужно будет добавить новые)
// 	defer func() {
// 		log.Println("App: Initiating shutdown sequence...")

// 		// Закрываем консьюмера задач парсинга ссылок
// 		if a.linksTaskConsumer != nil {
// 			log.Println("App: Closing Links Processing Task Consumer...")
// 			if err := a.linksTaskConsumer.Close(); err != nil {
// 				log.Printf("App: Error closing links processing task consumer: %v\n", err)
// 			}
// 		}
		
// 		// Продюсер закрывается один раз (он используется обоими адаптерами)
// 		if a.linksEventProducer != nil {
// 			log.Println("App: Closing Generic Event Producer...")
// 			// RabbitMQLinkQueueAdapter использует этот продюсер, поэтому его собственный Close() не нужен.
// 			if err := a.linksEventProducer.Close(); err != nil {
// 				log.Printf("App: Error closing generic event producer: %v\n", err)
// 			}
// 		}
		
// 		if a.dbPool != nil {
// 			log.Println("App: Closing PostgreSQL pool...")
// 			a.dbPool.Close()
// 		}
// 		log.Println("Application shut down gracefully.")
// 	}()

// 	log.Println("Application is starting...")

// 	// Запускаем сборщик ссылок Kufar
// 	// Используем context.Background() для простоты, в реальном приложении
// 	// может быть контекст с возможностью отмены извне.
// 	go a.StartKufarLinkFetcher(context.Background()) // Запускаем как горутину

// 	// Запускаем консьюмера задач для обработки ссылок
// 	consumerErrors := make(chan error, 1)
// 	go func() {
// 		log.Println("App: Starting Links Processing Task Consumer...")
// 		err := a.linksTaskConsumer.StartConsuming() // Этот метод блокирующий
// 		if err != nil {
// 			log.Printf("App: Links Processing Task Consumer stopped with error: %v", err)
// 			select {
// 			case consumerErrors <- fmt.Errorf("links processing task consumer error: %w", err):
// 			default:
// 			}
// 		} else {
// 			log.Println("App: Links Processing Task Consumer stopped gracefully.")
// 		}
// 	}()

// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

// 	log.Println("Application running. Waiting for signals or consumer error...")
// 	select {
// 	case receivedSignal := <-quit:
// 		log.Printf("App: Received signal: %s. Shutting down...\n", receivedSignal)
// 	case err := <-consumerErrors:
// 		log.Printf("App: A consumer stopped due to an error: %v. Shutting down...\n", err)
// 	}

// 	return nil
// }