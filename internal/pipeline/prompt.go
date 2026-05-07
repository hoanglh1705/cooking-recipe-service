package pipeline

import (
	"fmt"
	"strings"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
)

const composeSystem = `Bạn là chuyên gia ẩm thực Việt Nam. Bạn nhận được transcript từ vài video nấu ăn cùng một món, và phải tổng hợp lại thành MỘT công thức chuẩn, ngắn gọn, chính xác, KHÔNG copy nguyên văn. Trả lời CHỈ bằng JSON hợp lệ, không có markdown, không có giải thích.`

// BuildComposePrompt assembles the user prompt from the dish + video transcripts.
// We trim transcripts to a sensible cap so we stay inside reasonable token limits.
func BuildComposePrompt(dish db.Dish, videos []db.Video) (system, user string) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Món ăn: %s", dish.NameVi)
	if dish.NameEn != nil && *dish.NameEn != "" {
		fmt.Fprintf(&sb, " (%s)", *dish.NameEn)
	}
	if dish.Region != nil && *dish.Region != "" {
		fmt.Fprintf(&sb, " — %s", *dish.Region)
	}
	sb.WriteString("\n\n")

	count := 0
	for i, v := range videos {
		if v.Transcript == nil || strings.TrimSpace(*v.Transcript) == "" {
			continue
		}
		count++
		fmt.Fprintf(&sb, "[TRANSCRIPT %d]", i+1)
		if v.Channel != nil && *v.Channel != "" {
			fmt.Fprintf(&sb, " (kênh: %s)", *v.Channel)
		}
		sb.WriteString("\n")
		sb.WriteString(truncate(*v.Transcript, 6000))
		sb.WriteString("\n\n")
	}

	if count == 0 {
		sb.WriteString("[Không có transcript — hãy tạo công thức tiêu chuẩn dựa trên kiến thức của bạn về món này.]\n\n")
	}

	sb.WriteString(`Hãy tổng hợp thành MỘT công thức theo schema JSON dưới đây. Đảm bảo:
- "description" dài 150-160 ký tự, hấp dẫn, dùng luôn cho meta description SEO.
- "intro_md" 2-3 câu kể nguồn gốc/cảm hứng (giọng người, không AI-generic), markdown.
- "ingredients[].quantity" là số (number), KHÔNG phải string.
- Thời gian tính bằng phút (number). "total_time_min" = "prep_time_min" + "cook_time_min".
- "seo_title" tối đa 60 ký tự. "seo_keywords" 3-8 cụm từ long-tail.
- "difficulty" thuộc {"easy","medium","hard"}.

Schema:
{
  "title": "string",
  "description": "string (~150-160 chars)",
  "intro_md": "string",
  "prep_time_min": number,
  "cook_time_min": number,
  "total_time_min": number,
  "servings": number,
  "difficulty": "easy|medium|hard",
  "calories_per_serving": number | null,
  "ingredients": [{"name": "string", "quantity": number, "unit": "string", "note": "string"}],
  "steps": [{"instruction": "string", "duration_s": number, "tip": "string"}],
  "tips": ["string", ...],
  "variations": ["string", ...],
  "faqs": [{"q": "string", "a": "string"}],
  "seo_title": "string",
  "seo_keywords": ["string", ...]
}`)
	return composeSystem, sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
