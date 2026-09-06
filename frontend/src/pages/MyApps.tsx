/**
 * 自分が使えるアプリの一覧。
 *
 * 招待メールに URL を並べていたが、アプリが増えるたびに Terraform の
 * メール文面を直す必要があり、実際 3 アプリのまま古くなっていた。
 * **ログインした本人に、その人が許可されているものだけを出す**のが
 * いちばん確実なので、コンソールに置く。
 *
 * 誰が何を使えるかは Grant が持っているので、新しく持つ状態は無い。
 */
import { useQuery } from "@tanstack/react-query";

import { api, APPS } from "@/api/client";

export function MyApps({ email }: { email: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["grants", email],
    queryFn: () => api.listGrants(email),
    enabled: Boolean(email),
  });

  if (isLoading) return <div className="panel muted">読み込み中…</div>;
  if (error) {
    return (
      <div className="panel error">
        読み込みに失敗しました: {(error as Error).message}
      </div>
    );
  }

  const granted = data?.grants ?? [];

  // 許可されているアプリだけを、APPS の定義順で出す。
  // Grant の順（DynamoDB の返却順）に依存すると並びが安定しない。
  const mine = APPS.filter((a) => granted.some((g) => g.app === a.key));

  return (
    <div className="panel">
      <div className="row" style={{ marginBottom: "0.4rem" }}>
        <strong>使えるアプリ</strong>
        <span className="muted">{mine.length} 件</span>
      </div>

      {mine.length === 0 ? (
        <p className="muted">
          まだどのアプリも許可されていません。管理者にお問い合わせください。
        </p>
      ) : (
        <div className="app-links">
          {mine.map((app) => {
            const role = granted.find((g) => g.app === app.key)?.role;

            return (
              <a
                key={app.key}
                className="app-link"
                href={app.url}
                target="_blank"
                rel="noreferrer"
              >
                <img
                  className="app-icon"
                  src={`${app.url.replace(/\/$/, "")}${app.faviconPath}`}
                  alt=""
                  loading="lazy"
                  // 取得できなくても行が崩れないよう、枠だけ残して隠す。
                  // ファビコンの差し替えでパスが変わることがあるため。
                  onError={(e) => {
                    e.currentTarget.style.visibility = "hidden";
                  }}
                />
                <span className="app-body">
                  <span className="row" style={{ gap: 8, alignItems: "baseline" }}>
                    <strong>{app.label}</strong>
                    {role === "admin" && (
                      <span className="badge active">管理者</span>
                    )}
                    {role === "viewer" && <span className="badge">閲覧のみ</span>}
                  </span>
                  <span className="muted" style={{ fontSize: "0.8rem" }}>
                    {app.description}
                  </span>
                </span>
              </a>
            );
          })}
        </div>
      )}
    </div>
  );
}
