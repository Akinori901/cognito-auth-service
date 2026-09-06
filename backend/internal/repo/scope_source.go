package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
)

// ScopeSource は各アプリからスコープ候補を取ってくる。
//
// 中央はスコープの意味を知らない（不透明な文字列として扱う）。
// だが設定画面では実物を見て選びたいので、候補は各アプリに聞く。
// この分担により、アプリを増やしても中央のコードを変えずに済む。
type ScopeSource struct {
	// アプリごとの候補エンドポイント。空なら「候補なし」を返す。
	endpoints map[entity.AppKey]string
	// 各アプリと共有するシークレット。サーバー間の呼び出しなので
	// ユーザーの JWT を持てず、これで本人性を示す。
	secret string
	client *http.Client
}

func NewScopeSource(endpoints map[entity.AppKey]string, secret string) *ScopeSource {
	return &ScopeSource{
		endpoints: endpoints,
		secret:    secret,
		// 候補が取れなくても設定作業は続けられるべきなので、短めに切る。
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Candidate は選べるスコープ 1 つ分。
type Candidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// Fetch はアプリのスコープ候補を取る。
//
// エンドポイント未設定なら空を返す（エラーにしない）。
// 個人専有のアプリ（SkillLogger / dev-branding）は絞り込む対象が無いので、
// 「候補ゼロ」が正常な状態。
//
// email は「誰の候補か」。**アプリによって候補が人ごとに変わる。**
// task-scope のスペースはアプリ全体で共有だが、money-pilot の事業は
// 個人に属するため、誰の分かを伝えないと候補を決められない。
// 空でも渡す（アプリ側が無視するか空を返すかを決める）。
//
// 取得に失敗した場合はエラーを返す。呼び出し側は画面に
// 「候補を取得できません」と出し、**ロールの設定は続けられる**ようにする。
// 候補が取れないことを設定全体の妨げにしない。
func (s *ScopeSource) Fetch(ctx context.Context, app entity.AppKey, email string) ([]Candidate, error) {
	endpoint, ok := s.endpoints[app]
	if !ok || endpoint == "" {
		return []Candidate{}, nil
	}

	if email != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("候補取得先の URL が不正です: %w", err)
		}
		q := parsed.Query()
		q.Set("email", email)
		parsed.RawQuery = q.Encode()
		endpoint = parsed.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("リクエストの組み立てに失敗: %w", err)
	}

	if s.secret != "" {
		req.Header.Set("X-Console-Secret", s.secret)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s への問い合わせに失敗: %w", app, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s が %d を返しました", app, resp.StatusCode)
	}

	var body struct {
		Candidates []Candidate `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s の応答を読めません: %w", app, err)
	}

	if body.Candidates == nil {
		body.Candidates = []Candidate{}
	}
	return body.Candidates, nil
}
