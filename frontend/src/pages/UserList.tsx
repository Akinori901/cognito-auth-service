/**
 * ユーザー一覧。誰が何のアプリを使えるかを一望する。
 */
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, APPS, type User } from "@/api/client";

export function UserList({ onSelect }: { onSelect: (email: string) => void }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["users"],
    queryFn: () => api.listUsers(),
  });

  if (isLoading) return <div className="panel muted">読み込み中…</div>;
  if (error) {
    return <div className="panel error">読み込みに失敗しました: {(error as Error).message}</div>;
  }

  const users = data?.users ?? [];

  return (
    <div className="panel">
      <div className="row" style={{ marginBottom: "0.8rem" }}>
        <strong>ユーザー</strong>
        <span className="muted">{users.length} 件</span>
        <span className="spacer" style={{ marginLeft: "auto" }} />
        <button className="primary" onClick={() => onSelect("")}>
          ＋ 新規登録
        </button>
      </div>

      {users.length === 0 ? (
        <p className="muted">
          まだ誰も登録されていません。「新規登録」から追加してください。
        </p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>メールアドレス</th>
              <th>表示名</th>
              <th>状態</th>
              <th>ログイン</th>
              <th>コンソール</th>
              <th>メモ</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <UserRow key={u.email} user={u} onSelect={onSelect} />
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function UserRow({ user, onSelect }: { user: User; onSelect: (email: string) => void }) {
  return (
    <tr className="clickable" onClick={() => onSelect(user.email)}>
      <td>{user.email}</td>
      <td>{user.displayName || <span className="muted">—</span>}</td>
      <td>
        <span className={`badge ${user.status}`}>
          {user.status === "active" ? "有効" : "停止中"}
        </span>
      </td>
      <td>
        <span className="cell-inline">
          <CognitoBadge user={user} />
          <ResendButton user={user} />
        </span>
      </td>
      <td>
        {user.isConsoleAdmin ? (
          <span className="badge active">管理者</span>
        ) : (
          <span className="muted">—</span>
        )}
      </td>
      <td className="muted">{user.note}</td>
    </tr>
  );
}

/**
 * Cognito 側の状態バッジ。
 *
 * **この列がこの表の要**。台帳（他の列）と Cognito は自動では揃わないので、
 * 「登録済みなのにログインできない」を目で見て気付けるようにする。
 *
 * 招待中は残日数まで出す。仮パスワードは約 7 日で失効するため、
 * 失効してから気付くのでは遅い。
 */
function CognitoBadge({ user }: { user: User }) {
  const status = user.cognitoStatus;

  // 状態が取れなかった（Cognito への問い合わせが失敗した）。
  // 「未作成」と区別する。誤って作成し直すと招待メールが二重に飛ぶ。
  if (!status) return <span className="muted">不明</span>;

  if (status === "absent") {
    return (
      <span className="badge suspended" title="Cognito にアカウントが無いためログインできません">
        未作成
      </span>
    );
  }

  // Cognito 側で無効化されている場合は、状態より先にそれを出す。
  // 招待済みでも無効ならログインできないため。
  if (user.cognitoEnabled === false) {
    return (
      <span className="badge suspended" title="Cognito 側で無効化されています">
        無効
      </span>
    );
  }

  if (status === "invited") {
    return (
      <span className="cell-inline">
        {user.inviteExpired ? (
          <span
            className="badge suspended"
            title={`招待から ${user.inviteAgeDays} 日経過。仮パスワードは失効しています`}
          >
            失効
          </span>
        ) : (
          <span
            className="badge"
            title="招待メール送信済み・未ログイン"
          >
            招待中
          </span>
        )}
        <span className="muted" style={{ fontSize: "0.72rem" }}>
          {user.inviteExpired
            ? `${user.inviteAgeDays} 日経過`
            : user.inviteDaysLeft
              ? `あと ${user.inviteDaysLeft} 日`
              : ""}
        </span>
      </span>
    );
  }

  // Google 連携済みなら、ログイン経路が一目で分かるようにラベルへ含める。
  // 別バッジに分けると「利用中」と「Google」が別の意味に見えてしまう。
  return (
    <span className="cell-inline">
      <span
        className={`badge ${status === "confirmed" ? "active" : ""}`}
        title={
          user.googleLinked ? "Google ログインで利用しています" : undefined
        }
      >
        {status === "confirmed"
          ? user.googleLinked
            ? "利用中（Google）"
            : "利用中"
          : "要確認"}
      </span>
    </span>
  );
}

/**
 * 一覧から直接押せる再送ボタン。
 *
 * 失効を待たずに押せるようにしてある。迷惑メールに入って届いていない、
 * というケースが実際に多く、失効まで待たせる理由がないため。
 */
function ResendButton({ user }: { user: User }) {
  const qc = useQueryClient();
  const [done, setDone] = useState<string | null>(null);

  const resend = useMutation({
    mutationFn: () => api.resendInvite(user.email),
    onSuccess: () => {
      setDone("送信しました");
      void qc.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (e) => setDone((e as Error).message),
  });

  if (!user.canResend) return null;
  if (done) return <span className="muted" style={{ fontSize: "0.75rem" }}>{done}</span>;

  return (
    <button
      style={{ fontSize: "0.75rem", padding: "0.2rem 0.5rem" }}
      disabled={resend.isPending}
      onClick={(e) => {
        // 行クリックで詳細に飛ぶので、ボタンの分は伝播を止める
        e.stopPropagation();
        if (confirm(`${user.email} に招待メールを再送します。\n\n仮パスワードは新しい値に変わり、前のメールの値は使えなくなります。`)) {
          resend.mutate();
        }
      }}
    >
      {resend.isPending ? "送信中…" : "再送"}
    </button>
  );
}

/** 一覧に許可済みアプリを出すための表示名解決。 */
export function appLabel(key: string): string {
  return APPS.find((a) => a.key === key)?.label ?? key;
}
