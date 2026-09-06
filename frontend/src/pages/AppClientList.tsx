/**
 * App Client の一覧（読み取り専用）。
 *
 * どのアプリがこのプールに登録されているかを画面で確認するためのもの。
 * **作成・削除は Terraform の担当**なので、この画面に操作ボタンは無い。
 *
 * client_secret は API が返さないため、ここに表示する手段がそもそも無い。
 * 秘密情報が画面やブラウザ履歴に残らない構造にしてある。
 */
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/api/client";

export function AppClientList() {
  // 既定は畳んでおく。日常の運用で見るものではなく、
  // 「登録済みか確認したい」ときだけ開けばよい。
  const [open, setOpen] = useState(false);

  const { data, isLoading, error } = useQuery({
    queryKey: ["app-clients"],
    queryFn: () => api.listAppClients(),
    enabled: open,
  });

  const clients = data?.clients ?? [];

  return (
    <div className="panel" style={{ marginTop: "1rem" }}>
      <div className="row">
        <strong>登録アプリ（App Client）</strong>
        <span className="spacer" style={{ marginLeft: "auto" }} />
        <button onClick={() => setOpen(!open)}>{open ? "閉じる" : "表示する"}</button>
      </div>

      {open && (
        <>
          <p className="muted" style={{ marginTop: "0.4rem", fontSize: "0.8rem" }}>
            追加・削除は Terraform で行います（この画面からは変更できません）。
          </p>

          {isLoading ? (
            <p className="muted">読み込み中…</p>
          ) : error ? (
            <p className="error">取得に失敗しました: {(error as Error).message}</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>名前</th>
                  <th>Client ID</th>
                  <th>種別</th>
                </tr>
              </thead>
              <tbody>
                {clients.map((c) => (
                  <tr key={c.id}>
                    <td>{c.name}</td>
                    <td className="muted" style={{ fontFamily: "monospace", fontSize: "0.78rem" }}>
                      {c.id}
                    </td>
                    <td>
                      {c.hasSecret ? (
                        <span
                          className="badge"
                          title="client_secret を持つ（Custom GPT など）。値は CLI で取得します"
                        >
                          secret あり
                        </span>
                      ) : (
                        <span className="muted">SPA（PKCE）</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </div>
  );
}
