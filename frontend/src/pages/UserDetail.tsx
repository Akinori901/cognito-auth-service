/**
 * ユーザー詳細。基本情報と、アプリごとの許可・スコープを設定する。
 *
 * スコープは各アプリに候補を問い合わせてチェックボックスで出す。
 * 中央は文字列を配るだけで意味を知らないが、**設定するときは実物を見て選べる**
 * ようにするのがこの画面の役割。
 */
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, APPS, type AppKey, type Role, type User } from "@/api/client";

const EMPTY: Omit<User, "createdAt" | "updatedAt"> = {
  email: "",
  displayName: "",
  status: "active",
  isConsoleAdmin: false,
  note: "",
};

export function UserDetail({ email, onBack }: { email: string; onBack: () => void }) {
  const isNew = email === "";
  const qc = useQueryClient();
  const [form, setForm] = useState(EMPTY);
  const [msg, setMsg] = useState<string | null>(null);
  // 新規登録では既定で Cognito も作る。台帳だけ作っても本人はログインできず、
  // 「登録したのに入れない」が既定の結果になってしまうため。
  const [createCognito, setCreateCognito] = useState(true);

  const { data: users } = useQuery({
    queryKey: ["users"],
    queryFn: () => api.listUsers(),
    enabled: !isNew,
  });

  useEffect(() => {
    if (isNew) {
      setForm(EMPTY);
      return;
    }
    const u = users?.users.find((x) => x.email === email);
    if (u) setForm({ ...u });
  }, [isNew, email, users]);

  const save = useMutation({
    // Cognito を作るのは新規登録のときだけ。既存ユーザーの編集
    // （表示名の修正など）で毎回招待メールが飛ぶのを防ぐ。
    mutationFn: () => api.saveUser(form, isNew && createCognito),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ["users"] });

      // 台帳の保存は成功している。Cognito 側の結果を隠さず伝える。
      if (res.cognitoError) {
        setMsg(
          `台帳に登録しましたが、Cognito の作成に失敗しました（${res.cognitoError}）。` +
            "一覧の「ログイン」列から作り直せます。",
        );
        return; // 手当てが要るので画面に留まる
      }
      if (res.cognitoCreated) {
        setMsg("登録し、招待メールを送りました");
      } else if (res.cognitoSkipped) {
        setMsg("登録しました（Cognito には既にアカウントがあるため作成しませんでした）");
      } else {
        setMsg("保存しました");
      }

      // 新規登録なら、そのまま許可の設定に進めるよう詳細へ切り替える
      if (isNew) onBack();
      else setForm({ ...form, email: res.user.email });
    },
    onError: (e) => setMsg(`保存に失敗しました: ${(e as Error).message}`),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteUser(email),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["users"] });
      onBack();
    },
    onError: (e) => setMsg(`削除に失敗しました: ${(e as Error).message}`),
  });

  return (
    <>
      <div className="row" style={{ marginBottom: "1rem" }}>
        <button onClick={onBack}>← 一覧へ</button>
        <strong>{isNew ? "新規登録" : email}</strong>
      </div>

      <div className="panel">
        <div className="field">
          <label>メールアドレス</label>
          <input
            type="email"
            value={form.email}
            // 既存ユーザーの email は変えられない。変えると別レコードになり、
            // 元のユーザーと許可が孤立するため。
            disabled={!isNew}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            placeholder="user@example.com"
          />
          {!isNew && (
            <span className="muted" style={{ fontSize: "0.78rem" }}>
              メールアドレスは変更できません（別のユーザーとして扱われるため）
            </span>
          )}
        </div>

        <div className="field">
          <label>表示名</label>
          <input
            type="text"
            value={form.displayName}
            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
          />
        </div>

        <div className="field">
          <label>状態</label>
          <select
            value={form.status}
            onChange={(e) => setForm({ ...form, status: e.target.value as User["status"] })}
          >
            <option value="active">有効</option>
            <option value="suspended">停止中（全アプリで拒否）</option>
          </select>
        </div>

        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={form.isConsoleAdmin}
              onChange={(e) => setForm({ ...form, isConsoleAdmin: e.target.checked })}
              style={{ width: "auto", marginRight: 6 }}
            />
            このコンソールを操作できる
          </label>
          <span className="muted" style={{ fontSize: "0.78rem" }}>
            Google ログインであることも必要です
          </span>
        </div>

        <div className="field">
          <label>メモ</label>
          <input
            type="text"
            value={form.note}
            onChange={(e) => setForm({ ...form, note: e.target.value })}
            placeholder="所属・用途など"
          />
        </div>

        {isNew && (
          <div className="field">
            <label>
              <input
                type="checkbox"
                checked={createCognito}
                onChange={(e) => setCreateCognito(e.target.checked)}
                style={{ width: "auto", marginRight: 6 }}
              />
              Cognito にアカウントを作り、招待メールを送る
            </label>
            <span className="muted" style={{ fontSize: "0.78rem" }}>
              {createCognito
                ? "Google ログインを使う場合もこの作成が必要です（未作成だとログインを拒否します）"
                : "作成しない場合、この人は台帳に載るだけでログインできません"}
            </span>
          </div>
        )}

        <div className="row">
          <button
            className="primary"
            disabled={!form.email || save.isPending}
            onClick={() => save.mutate()}
          >
            {save.isPending ? "保存中…" : "保存"}
          </button>
          {!isNew && (
            <button
              className="danger"
              disabled={remove.isPending}
              onClick={() => {
                if (confirm(`${email} を削除します。許可もすべて消えます。`)) remove.mutate();
              }}
            >
              削除
            </button>
          )}
          {msg && <span className="muted">{msg}</span>}
        </div>
      </div>

      {!isNew && <CognitoPanel email={email} />}
      {!isNew && <GrantEditor email={email} />}
    </>
  );
}

// --- Cognito（ログインの関門）---------------------------------------------

/**
 * Cognito 側の状態と操作。
 *
 * 台帳（上のパネル）とは別の関門であることを画面でも分けて示す。
 * 台帳の「停止中」は各アプリの認可を拒否するだけで、Cognito の
 * ログイン自体は通る。即座に止めたいときはこちらを無効にする。
 */
function CognitoPanel({ email }: { email: string }) {
  const qc = useQueryClient();
  const [msg, setMsg] = useState<string | null>(null);

  // 一覧の取得結果を使い回す。詳細のためだけに再取得しない。
  const { data } = useQuery({ queryKey: ["users"], queryFn: () => api.listUsers() });
  const user = data?.users.find((u) => u.email === email);
  const status = user?.cognitoStatus;

  const refresh = () => void qc.invalidateQueries({ queryKey: ["users"] });

  const create = useMutation({
    mutationFn: () => api.createCognitoUser(email),
    onSuccess: (r) => {
      setMsg(r.alreadyExists ? "既にアカウントがあります" : "招待メールを送りました");
      refresh();
    },
    onError: (e) => setMsg(`作成に失敗しました: ${(e as Error).message}`),
  });

  const resend = useMutation({
    mutationFn: () => api.resendInvite(email),
    onSuccess: () => {
      setMsg("招待メールを再送しました（仮パスワードは新しい値に変わりました）");
      refresh();
    },
    onError: (e) => setMsg(`再送に失敗しました: ${(e as Error).message}`),
  });

  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api.setCognitoEnabled(email, enabled),
    onSuccess: (r) => {
      setMsg(r.enabled ? "有効にしました" : "無効にしました（ログインできなくなります）");
      refresh();
    },
    onError: (e) => setMsg(`変更に失敗しました: ${(e as Error).message}`),
  });

  return (
    <div className="panel">
      <strong>ログイン（Cognito）</strong>
      <p className="muted" style={{ marginTop: "0.2rem" }}>
        ここに無いとログインできません。上の「状態」とは別の関門です。
      </p>

      {!status ? (
        <p className="muted">状態を取得できませんでした。</p>
      ) : status === "absent" ? (
        <>
          <p className="error" style={{ fontSize: "0.85rem" }}>
            Cognito にアカウントがありません。このままではログインできません。
          </p>
          <button
            className="primary"
            disabled={create.isPending}
            onClick={() => {
              if (confirm(`${email} に招待メールを送ります。`)) create.mutate();
            }}
          >
            {create.isPending ? "作成中…" : "アカウントを作って招待メールを送る"}
          </button>
        </>
      ) : (
        <>
          {status === "invited" && (
            <p
              className={user?.inviteExpired ? "error" : undefined}
              style={{ fontSize: "0.9rem" }}
            >
              {user?.inviteExpired
                ? `招待から ${user?.inviteAgeDays} 日経過し、仮パスワードは失効しています。` +
                  "再送すると新しい仮パスワードが発行されます。"
                : `招待済み・未ログイン（あと ${user?.inviteDaysLeft} 日で失効します）`}
            </p>
          )}
          {status !== "invited" && (
            <p style={{ fontSize: "0.9rem" }}>
              {status === "confirmed" &&
                (user?.googleLinked
                  ? "利用中（Google ログイン）"
                  : "利用中（メール + パスワード）")}
              {status === "other" && "確認が必要な状態です"}
            </p>
          )}
          <div className="row">
            {user?.canResend && (
              <button
                disabled={resend.isPending}
                onClick={() => {
                  if (
                    confirm(
                      `${email} に招待メールを再送します。\n\n` +
                        "仮パスワードは新しい値に変わり、前のメールの値は使えなくなります。",
                    )
                  ) {
                    resend.mutate();
                  }
                }}
              >
                {resend.isPending ? "送信中…" : "招待メールを再送する"}
              </button>
            )}
            {user?.cognitoEnabled === false ? (
              <button disabled={toggle.isPending} onClick={() => toggle.mutate(true)}>
                有効にする
              </button>
            ) : (
              <button
                className="danger"
                disabled={toggle.isPending}
                onClick={() => {
                  if (confirm(`${email} のログインを止めます。`)) toggle.mutate(false);
                }}
              >
                無効にする（すぐログインできなくなります）
              </button>
            )}
          </div>
        </>
      )}

      {msg && (
        <p className="muted" style={{ marginTop: "0.6rem" }}>
          {msg}
        </p>
      )}
    </div>
  );
}

// --- アプリごとの許可 -----------------------------------------------------

function GrantEditor({ email }: { email: string }) {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["grants", email],
    queryFn: () => api.listGrants(email),
  });

  if (isLoading) return <div className="panel muted">許可を読み込み中…</div>;

  const grants = data?.grants ?? [];

  return (
    <div className="panel">
      <strong>使えるアプリ</strong>
      <p className="muted" style={{ marginTop: "0.2rem" }}>
        チェックを外すと、そのアプリは使えなくなります。
      </p>

      {APPS.map((app) => (
        <AppGrant
          key={app.key}
          email={email}
          appKey={app.key}
          appLabel={app.label}
          grant={grants.find((g) => g.app === app.key)}
          onChanged={() => void qc.invalidateQueries({ queryKey: ["grants", email] })}
        />
      ))}
    </div>
  );
}

function AppGrant({
  email,
  appKey,
  appLabel,
  grant,
  onChanged,
}: {
  email: string;
  appKey: AppKey;
  appLabel: string;
  grant?: { role: Role; scopes: string[] };
  onChanged: () => void;
}) {
  const enabled = Boolean(grant);
  const [role, setRole] = useState<Role>(grant?.role ?? "member");
  const [scopes, setScopes] = useState<string[]>(grant?.scopes ?? []);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    setRole(grant?.role ?? "member");
    setScopes(grant?.scopes ?? []);
  }, [grant]);

  // 候補は各アプリに問い合わせる。中央はスコープの意味を知らないため。
  const { data: candidates, error: candErr } = useQuery({
    queryKey: ["scope-candidates", appKey, email],
    queryFn: () => api.scopeCandidates(appKey, email),
    enabled,
    retry: false,
  });

  const save = useMutation({
    mutationFn: (next: { role: Role; scopes: string[] }) =>
      api.saveGrant({ email, app: appKey, role: next.role, scopes: next.scopes }),
    onSuccess: () => {
      setErr(null);
      onChanged();
    },
    onError: (e) => setErr((e as Error).message),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteGrant(email, appKey),
    onSuccess: () => {
      setErr(null);
      onChanged();
    },
    onError: (e) => setErr((e as Error).message),
  });

  return (
    <div style={{ borderTop: "1px solid var(--border)", padding: "0.8rem 0" }}>
      <div className="row">
        <label style={{ display: "flex", gap: 6, alignItems: "center" }}>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => {
              if (e.target.checked) save.mutate({ role: "member", scopes: [] });
              else remove.mutate();
            }}
          />
          <strong>{appLabel}</strong>
        </label>

        {enabled && (
          <>
            <select
              value={role}
              style={{ width: "auto" }}
              onChange={(e) => {
                const next = e.target.value as Role;
                setRole(next);
                save.mutate({ role: next, scopes });
              }}
            >
              <option value="admin">管理者</option>
              <option value="member">メンバー</option>
              <option value="viewer">閲覧のみ</option>
            </select>
            {save.isPending && <span className="muted">保存中…</span>}
          </>
        )}
      </div>

      {enabled && (
        <div className="scopes">
          {candErr ? (
            <span className="muted">
              候補を取得できません（アプリ側が未対応か応答なし）。
              現在の設定: {scopes.length > 0 ? scopes.join(", ") : "絞り込みなし"}
            </span>
          ) : (candidates?.candidates.length ?? 0) === 0 ? (
            <span className="muted">
              このアプリは範囲の絞り込みがありません（全データが対象）
            </span>
          ) : (
            candidates!.candidates.map((c) => (
              <label key={c.id}>
                <input
                  type="checkbox"
                  checked={scopes.includes(c.id)}
                  onChange={(e) => {
                    const next = e.target.checked
                      ? [...scopes, c.id]
                      : scopes.filter((s) => s !== c.id);
                    setScopes(next);
                    save.mutate({ role, scopes: next });
                  }}
                />
                {c.label}
                <span className="muted" style={{ fontSize: "0.75rem" }}>
                  {c.id}
                </span>
              </label>
            ))
          )}
        </div>
      )}

      {err && <div className="error" style={{ fontSize: "0.8rem" }}>{err}</div>}
    </div>
  );
}
