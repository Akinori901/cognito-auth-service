/**
 * 認証ゲート。ログイン状態とコンソール権限を確かめてから中身を出す。
 *
 * 権限の判定はサーバー側（/api/console/me が 403 を返す）が正で、
 * ここはその結果を画面に出すだけ。**フロントで判定してはいけない**
 * （ブラウザの JS は書き換えられる）。
 */
import { useEffect, useState, type ReactNode } from "react";
import { signInWithRedirect, signOut, getCurrentUser } from "aws-amplify/auth";

import { api } from "@/api/client";
import { isCognitoConfigured } from "@/auth/amplify-config";

type State =
  | { kind: "loading" }
  | { kind: "signedOut" }
  | { kind: "forbidden"; message: string }
  | { kind: "ready"; email: string; provider: string };

export function AuthGate({ children }: { children: (email: string) => ReactNode }) {
  const [state, setState] = useState<State>({ kind: "loading" });

  useEffect(() => {
    void check();
  }, []);

  async function check() {
    setState({ kind: "loading" });

    if (!isCognitoConfigured) {
      setState({
        kind: "forbidden",
        message: "Cognito が未設定です。VITE_COGNITO_* を .env に設定してください。",
      });
      return;
    }

    try {
      await getCurrentUser();
    } catch {
      setState({ kind: "signedOut" });
      return;
    }

    try {
      // ここでサーバーに権限を問う。403 ならコンソールに入れない。
      const me = await api.me();
      setState({ kind: "ready", email: me.email, provider: me.provider });
    } catch (e) {
      setState({ kind: "forbidden", message: (e as Error).message });
    }
  }

  if (state.kind === "loading") {
    return <div className="center muted">確認しています…</div>;
  }

  if (state.kind === "signedOut") {
    return (
      <div className="center">
        <div>
          <h1>認証コンソール</h1>
          <p className="muted">
            Google アカウントでログインしてください。
            <br />
            メール＋パスワードのアカウントではこの画面に入れません。
          </p>
          <button
            className="primary"
            onClick={() => void signInWithRedirect({ provider: "Google" })}
          >
            Google でログイン
          </button>
        </div>
      </div>
    );
  }

  if (state.kind === "forbidden") {
    return (
      <div className="center">
        <div>
          <h1>利用できません</h1>
          <p className="muted">{state.message}</p>
          <button onClick={() => void signOut()}>ログアウト</button>
        </div>
      </div>
    );
  }

  return (
    <>
      <header className="header">
        <h1>認証コンソール</h1>
        <span className="spacer" />
        <span className="muted">
          {state.email}
          <span className="badge" style={{ marginLeft: 8 }}>
            {state.provider}
          </span>
        </span>
        <button onClick={() => void signOut()}>ログアウト</button>
      </header>
      {children(state.email)}
    </>
  );
}
