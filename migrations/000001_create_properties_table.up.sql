-- Расширение для генерации UUID, если его еще нет в БД
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS properties (
    -- Системные поля
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ,
    deleted_at              TIMESTAMPTZ,
    is_duplicated           BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_id           UUID, -- REFERENCES public.managers(id)
    published_by_id         UUID, -- REFERENCES public.managers(id)
    responsible_manager_id  UUID, -- REFERENCES public.managers(id)
    partner                 VARCHAR NOT NULL,

    -- Основные данные объявления
    source                  VARCHAR NOT NULL,
    slug                    VARCHAR NOT NULL,
    title                   VARCHAR NOT NULL,
    description             TEXT,
    images                  TEXT[], -- Массив строк
    preview_image           VARCHAR,
    address                 VARCHAR NOT NULL,
    district                VARCHAR,
    coordinates             JSONB,
    contacts                JSONB,

    -- Типы и статусы
    transaction_type        VARCHAR,
    estate                  VARCHAR,
    advert_type             VARCHAR,
    advertiser_type         VARCHAR,
    status                  VARCHAR NOT NULL DEFAULT 'active',

    -- Цена
    total_price             NUMERIC(20, 2),
    rent_price              NUMERIC(20, 2),
    deposit_price           NUMERIC(20, 2),
    price_per_square_meter  NUMERIC(20, 2),
    currency                VARCHAR,

    -- Характеристики недвижимости
    area_in_square_meters   NUMERIC(20, 2),
    rooms_number_string     VARCHAR,
    rooms_num               TEXT[], -- Массив строк
    floor_number            INT4,
    building_floors         INT4,
    note                    TEXT,
    features                TEXT[],
    parsed_features         TEXT[],
    marks                   TEXT[],

    -- Даты с сайта
    published_at            TIMESTAMPTZ,
    site_created_at         TIMESTAMPTZ NOT NULL,
    site_updated_at         TIMESTAMPTZ NOT NULL
);