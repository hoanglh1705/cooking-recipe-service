# cooking-recipe-service

Backend Go cho dự án "Món Ngon Mỗi Ngày". Triển khai theo `architech.md` mục **2.2. Backend — Go**.

## Hai binary

| Binary | Vai trò |
|---|---|
| `cmd/api` | HTTP server (Echo) — `/api/*` cho Next.js, `/api/admin/*` cho admin panel, `/api/internal/*` cho crawler |
| `cmd/worker` | Asynq worker chạy 5 stage pipeline AI |

Mọi service dùng chung cấu hình qua biến môi trường (xem `.env.example`).

## Cấu trúc

```
cooking-recipe-service/
├── cmd/
│   ├── api/           # HTTP server
│   └── worker/        # Asynq pipeline worker
├── internal/
│   ├── api/           # /api/* — handlers public cho Next.js
│   ├── admin/         # /api/admin/* — handlers quản trị (Bearer token)
│   ├── internalapi/   # /api/internal/* — handlers crawler (HMAC)
│   ├── clients/
│   │   ├── youtube/   # YouTube Data API v3
│   │   ├── tiktok/    # placeholder (xem mục pháp lý)
│   │   ├── llm/       # Anthropic / OpenAI compose JSON
│   │   └── whisper/   # OpenAI whisper-1 transcription
│   ├── config/        # nạp biến môi trường
│   ├── db/            # pgx pool, migrations embed, query layer
│   ├── pipeline/      # 5 stage handlers + prompt builder
│   └── queue/         # Asynq client + payload types
└── internal/db/migrations/  # SQL migrations chạy tự động khi khởi động
```

## Endpoints

### Public (`/api/*`)

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/health` | Health check |
| GET | `/api/recipes` | List recipe summaries (`?limit=`, `?category=<slug>`) |
| GET | `/api/recipes/slugs` | Tất cả slug đã publish — Next.js `generateStaticParams` |
| GET | `/api/recipes/search?q=` | Search theo tên/keyword |
| GET | `/api/recipes/:slug` | Chi tiết công thức (ingredients/steps/videos/categories) |
| GET | `/api/dishes` | List dishes (`?category=<slug>&tag=<tag>&region=<slug>&has_recipe=true&limit=&offset=`) |
| GET | `/api/dishes/:slug` | Chi tiết dish (kèm `has_recipe` + categories) |
| GET | `/api/categories` | Tất cả category đang `visible` (rich objects với tags + sort_order + recipe_count) |
| GET | `/api/categories/:slug` | Chi tiết 1 category |
| GET | `/api/categories/:slug/recipes` | Recipes của category đó (`?limit=`) |
| GET | `/api/tags` | Union tag từ `dishes.keywords` + `categories.tags` (case-insensitive distinct) |
| GET | `/api/home` | Sections cho trang chủ — mỗi section là 1 category `show_on_home` + recipes (`?per_section=8`) |

#### Filter dishes theo tag/category

`GET /api/dishes?tag=phở` match case-insensitive với:
- `dishes.keywords` của bản thân dish, **HOẶC**
- `categories.tags` của bất kỳ category nào dish đang thuộc về.

Nên `?tag=miền+Bắc` (alias trong category `mon-bac.tags`) sẽ trả về tất cả dish nằm trong category đó, kể cả dish không có "miền Bắc" trong `keywords` của riêng nó.

`?has_recipe=true` để chỉ lấy dish đã có công thức publish (filter "coming soon" out). Bỏ qua param này = lấy tất cả.

### Admin (`/api/admin/*`) — yêu cầu `Authorization: Bearer $ADMIN_TOKEN`

Spec đầy đủ ở [docs/admin_architech.md §6](../docs/admin_architech.md). Path đã đổi
từ `/admin/*` sang `/api/admin/*` để khớp doc. JWT cookie auth (login/logout/refresh)
sẽ thay thế Bearer ở phase sau — hiện đang dùng Bearer cho MVP.

#### Auth + me

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/me` | Trả user stub (chế độ Bearer MVP) |

#### Dishes (§6.2)

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/dishes` | List paginated `?q=&status=&category=&region=&page=&page_size=&sort=` → `{items,total,page,page_size}` |
| POST | `/api/admin/dishes` | Body `{name_vi, slug, name_en?, category_id?, region?, tag_ids[], keywords[]}` |
| GET | `/api/admin/dishes/:id` | Bundle `{dish, categories, tags, videos, events}` |
| PATCH | `/api/admin/dishes/:id` | Partial update (name_vi, slug, region, keywords, status…) |
| DELETE | `/api/admin/dishes/:id` | Soft-delete → `status=archived` |
| POST | `/api/admin/dishes/:id/trigger-crawler` | Đẩy job vào queue (rename từ `/pipeline`) |
| POST | `/api/admin/dishes/:id/retry` | Reset về `pending` + recipe (nếu có) → `discarded` |
| GET / PUT | `/api/admin/dishes/:id/categories` | List/Set categories (body `{category_ids:[]}`) |
| GET / PUT | `/api/admin/dishes/:id/tags` | List/Set tags (body `{tag_ids:[]}`) |

#### Recipes (§6.3) — admin review queue

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/recipes` | List paginated `?status=&dish_id=&page=&page_size=` |
| GET | `/api/admin/recipes/:id` | Full review payload `{recipe, original_recipe, source_videos}` |
| PATCH | `/api/admin/recipes/:id` | Partial update (title/desc/intro_md/steps/ingredients…) |
| POST | `/api/admin/recipes/:id/publish` | Set published + trigger Next.js ISR revalidate |
| POST | `/api/admin/recipes/:id/unpublish` | Đảo lại publish |
| POST | `/api/admin/recipes/:id/reject` | Body `{reason}` → recipe=rejected, dish=failed |

#### Categories (§6.4)

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/categories` | List tất cả (kể cả ẩn) |
| POST | `/api/admin/categories` | Tạo category (upsert theo slug) |
| GET | `/api/admin/categories/:id` | Chi tiết |
| PATCH | `/api/admin/categories/:id` | Cập nhật (slug bất biến) |
| DELETE | `/api/admin/categories/:id` | Xoá |
| POST | `/api/admin/categories/reorder` | Body `{ordered_ids:[1,2,3]}` (canonical) hoặc `{slugs:[]}` (alias) |

#### Tags (§6.4) — first-class taxonomy

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/tags?q=&limit=` | Search tags (substring + sort_order) |
| POST | `/api/admin/tags` | Body `{slug, name, description?, sort_order?}` |
| GET / PATCH / DELETE | `/api/admin/tags/:id` | CRUD; slug bất biến |
| POST | `/api/admin/tags/merge` | Body `{source_id, target_id}` — chuyển dish + xoá source |

#### Events & Stats (§6.5)

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/admin/events` | `?kind=&dish_id=&from=&to=&limit=` (RFC3339 timestamps) |
| GET | `/api/admin/stats/overview` | Counts theo status — cho dashboard cards |
| GET | `/api/admin/stats/pipeline` | 7 ngày gần nhất, started/failed per day |
| GET | `/api/admin/stats/cost` | MTD + last month LLM cost |

> **Format error chuẩn** ở mọi endpoint admin: `{"error":{"code":"...","message":"...","fields":{...}}}`.
> `fields` chỉ xuất hiện ở `VALIDATION_FAILED` để FE map vào input tương ứng.

### Internal (`/api/internal/*`) — yêu cầu HMAC `X-Crawler-Auth`

Bảo vệ bằng `HMAC-SHA256(CRAWLER_SECRET, timestamp + body)`. Spec đầy đủ trong [docs/crawler_architech.md §6](../docs/crawler_architech.md).

| Method | Path | Mô tả |
|---|---|---|
| GET | `/api/internal/dishes/pending?limit=` | List dish pending (oldest-first), tự reclaim lock hết hạn |
| GET | `/api/internal/dishes/:id` | Chi tiết dish (kèm lock state + last_error_*) |
| POST | `/api/internal/dishes/:id/lock` | Acquire/renew TTL lock — body `{worker_id, ttl_seconds}` |
| POST | `/api/internal/dishes/:id/unlock` | Release lock + set `final_status` ∈ {`ready_for_review`, `failed`, `pending`} |
| POST | `/api/internal/recipes` | Upsert recipe + source videos. Idempotent qua header `Idempotency-Key` |
| POST | `/api/internal/events` | Append vào `pipeline_events` để admin debug |

## Pipeline AI

Khớp với 5 stage trong `architech.md`. Mỗi stage là một asynq task riêng → retry độc lập.

```
search_videos → download_audio → transcribe_video → compose_recipe → publish_recipe
```

- `compose_recipe` chỉ chạy khi *tất cả* video của dish có transcript.
- `publish_recipe` chấm `dishes.status = 'published'`, gọi Next.js revalidate webhook nếu có.
- Khi LLM/Whisper API key trống, các client trả về stub deterministic — pipeline vẫn chạy được end-to-end để test.

## Chạy local

```bash
cp .env.example .env
# chỉnh ADMIN_TOKEN, các API key (DATABASE_URL/REDIS_ADDR mặc định khớp docker-compose)

make infra-up          # Postgres + Redis + Asynqmon (dashboard)
make tidy
make build
make run-seed          # bơm 20 món Việt phổ biến vào bảng dishes

# Terminal 1
./bin/api

# Terminal 2 (worker — cần yt-dlp trong $PATH nếu muốn chạy stage download)
./bin/worker
```

Migration chạy tự động khi binary khởi động (idempotent — `CREATE … IF NOT EXISTS`).

### docker-compose

| Service | Cổng | Ghi chú |
|---|---|---|
| `postgres` | `5432` | user/pass/db = `postgres/postgres/cooking_recipe` |
| `redis` | `6379` | AOF persistence |
| `asynqmon` | `8081` | Web UI để xem queue, retry, fail |

```bash
make infra-up      # khởi
make infra-logs    # tail log
make infra-down    # stop, giữ data
make infra-reset   # stop + xoá volume (DB trống lại)
```

### Seed dữ liệu

`make run-seed` chạy [cmd/seed](cmd/seed/) — bơm 3 thứ:

1. **~20 món Việt phổ biến** ([cmd/seed/main.go](cmd/seed/main.go)) — Phở bò, Bún chả, Bún bò Huế, Mì Quảng, Cao lầu, Cơm tấm, Hủ tiếu Nam Vang, Bánh xèo, Canh chua cá lóc, Cá kho tộ, Thịt kho hột vịt, Gỏi cuốn, Chả giò, … phủ đủ 3 miền.
2. **11 category cho trang chủ** ([cmd/seed/categories.go](cmd/seed/categories.go)) — Bữa sáng · Cơm · Phở & Bún · Canh & Súp · Món kho · Nướng & Chiên · Cuốn & Gỏi · Khai vị · Món Bắc · Món Trung · Món Nam. Mỗi category có `tags` riêng (alias/keywords cho SEO), `icon` (lucide name) và `sort_order` quyết định thứ tự xuất hiện trên home.
3. **Mapping dish ↔ category** — một dish có thể nằm ở nhiều section (ví dụ Phở bò Hà Nội = Bữa sáng + Phở & Bún + Món Bắc).

Idempotent:
- Dishes: `ON CONFLICT (slug) DO NOTHING` — giữ nguyên dữ liệu cũ.
- Categories: `ON CONFLICT (slug) DO UPDATE` — cập nhật metadata khi sửa seed (tags, sort_order, show_on_home).
- Links: `ON CONFLICT DO NOTHING`.

## Phụ thuộc runtime

- **PostgreSQL** ≥ 14 (hỗ trợ array & TIMESTAMPTZ).
- **Redis** ≥ 6 cho Asynq queue.
- **`yt-dlp`** binary trong `$PATH` (chỉ worker cần khi chạy stage download).
- **API keys** (tuỳ chọn): YouTube Data API v3, Anthropic hoặc OpenAI, OpenAI Whisper.

## Test nhanh không cần secrets

1. `make infra-up && make run-seed`
2. `./bin/api &` rồi `./bin/worker &`
3. Trigger pipeline cho dish đã seed:
   ```bash
   curl -X POST localhost:8080/admin/dishes/1/pipeline \
     -H "Authorization: Bearer $ADMIN_TOKEN"
   ```
4. Theo dõi job ở **http://localhost:8081** (Asynqmon).
5. Đọc kết quả khi worker xong:
   ```bash
   curl localhost:8080/api/recipes/pho-bo-ha-noi | jq
   ```
# cooking-recipe-service
