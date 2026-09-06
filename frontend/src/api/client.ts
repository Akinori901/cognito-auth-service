/**
 * 認証コンソール API のクライアント。
 *
 * バックエンドは ID トークンを検証する（アクセストークンではない）。
 * コンソールの認可には email と identities（IdP 種別）が要るが、
 * Cognito のアクセストークンにはどちらも入らないため。
 */
import { fetchAuthSession } from "aws-amplify/auth";

import { isCognitoConfigured } from "@/auth/amplify-config";

/** アプリの識別子。バックエンドの entity.AppKey と揃える。 */
/**
 * 収容しているアプリ。
 *
 * url / 説明 / ファビコンをここで持つ。アプリの追加は年に数回なので
 * DB 化はせずコードで管理する（レビューが残る）。
 *
 * faviconPath は各アプリが実際に配信しているパス。アプリごとに
 * 置き場所が違う（favicon.ico / favicon-32x32.png / icon.svg）ので
 * 決め打ちにできない。SkillLogger の favicon.ico は 400KB 近くあるため、
 * 一覧で 6 枚並べる用途では軽い 32x32 の方を指している。
 */
export const APPS = [
  {
    key: "task-scope",
    label: "Task Scope",
    url: "https://example-app-1.cloudfront.net/",
    description: "Backlog / Jira のチケットを横断して見る",
    faviconPath: "/favicon-32x32.png",
  },
  {
    key: "skilllogger",
    label: "SkillLogger",
    url: "https://example-app-2.cloudfront.net/",
    description: "案件と技術の記録からスキルシートを作る",
    faviconPath: "/favicon.ico",
  },
  {
    key: "money-pilot",
    label: "MoneyPilot",
    url: "https://example-app-3.cloudfront.net/",
    description: "事業の入出金を管理する",
    faviconPath: "/favicon.ico",
  },
  {
    key: "dev-branding",
    label: "Publicity",
    url: "https://example-app-4.cloudfront.net/",
    description: "案件の知見から記事を作って配信する",
    faviconPath: "/favicon.ico",
  },
  {
    key: "fair-value-calculator",
    label: "Fair Value Calculator",
    url: "https://example-app-5.cloudfront.net/",
    description: "株式の適正価格を算出する",
    faviconPath: "/favicon.ico",
  },
  {
    key: "memory-nest",
    label: "Memory Nest",
    url: "https://example-app-6.cloudfront.net/",
    description: "写真と動画を家族で残す",
    faviconPath: "/icon.svg",
  },
] as const;

export type AppKey = (typeof APPS)[number]["key"];

export type Role = "admin" | "member" | "viewer";

/**
 * Cognito 側の状態。台帳（User）とは別の関門なので、揃っているとは限らない。
 *
 * absent    = Cognito に居ない → **ログインできない**（作成が必要）
 * invited   = 招待済みだが未ログイン（仮パスワードは既定 7 日で失効）
 * confirmed = ログイン実績あり
 * other     = それ以外（パスワードリセット待ちなど）
 */
export type CognitoStatus = "absent" | "invited" | "confirmed" | "other";

export interface User {
  email: string;
  displayName: string;
  status: "active" | "suspended";
  isConsoleAdmin: boolean;
  note: string;
  createdAt: string;
  updatedAt: string;

  /** 取得できなかった場合は undefined（画面は「不明」と出す）。 */
  cognitoStatus?: CognitoStatus;
  cognitoEnabled?: boolean;
  googleLinked?: boolean;

  /** 招待の失効まわり。cognitoStatus が "invited" のときだけ意味を持つ。 */
  inviteExpired?: boolean;
  inviteDaysLeft?: number;
  inviteAgeDays?: number;
  /** 再送できるか（ログイン済み・外部 IdP のみの人には送れない）。 */
  canResend?: boolean;
}

/** 登録の結果。台帳と Cognito は成否が別々になりうる。 */
export interface SaveUserResult {
  user: User;
  cognitoCreated: boolean;
  cognitoSkipped: boolean;
  cognitoError: string;
}

export interface Grant {
  email: string;
  app: AppKey;
  role: Role;
  scopes: string[];
  createdAt: string;
  updatedAt: string;
}

/**
 * このプールに登録されている App Client。
 *
 * **client_secret は含まれない。** 有無だけが分かる。値が要るときは
 * CLI で取得する（コンソールは秘密情報を扱わない）。
 */
export interface AppClient {
  id: string;
  name: string;
  hasSecret: boolean;
}

/** 各アプリが返すスコープの候補。コンソールはこれをチェックボックスで出す。 */
export interface ScopeCandidate {
  id: string;
  label: string;
  kind: string;
}

async function authHeader(): Promise<Record<string, string>> {
  if (!isCognitoConfigured) return {};
  try {
    const session = await fetchAuthSession();
    // ID トークンを送る。アクセストークンには email も identities も入らない。
    const token = session.tokens?.idToken?.toString();
    return token ? { Authorization: `Bearer ${token}` } : {};
  } catch {
    return {};
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, {
    ...init,
    headers: {
      "content-type": "application/json",
      ...(await authHeader()),
      ...(init?.headers ?? {}),
    },
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    // 403 は「認証は通ったが権限が無い」。理由はサーバー側で伏せているので
    // ここでも推測を足さない。
    throw new Error(body.error ?? `${res.status} ${res.statusText}`);
  }

  // 削除系は 204 No Content を返す。本文が無いので json() を呼ぶと
  // 「Unexpected end of JSON input」になる。成功しているのに失敗として
  // 表示されるため、本文の有無で分ける。
  if (res.status === 204 || res.headers.get("content-length") === "0") {
    return undefined as T;
  }
  return res.json() as Promise<T>;
}

export const api = {
  /**
   * App Client の一覧（読み取りのみ）。
   *
   * 作成・削除は Terraform の担当。コンソールからは変更できない。
   */
  listAppClients: () => req<{ clients: AppClient[] }>("/console/app-clients"),

  /** ログイン中の本人情報。コンソールに入れるかの確認も兼ねる。 */
  me: () => req<{ email: string; provider: string }>("/console/me"),

  listUsers: () => req<{ users: User[] }>("/console/users"),

  /**
   * 台帳に登録する。createCognito が true なら Cognito にも作成し、
   * 招待メールを送る。
   *
   * **台帳の保存が成功していれば 200 が返る。** Cognito の作成だけが
   * 失敗した場合も 200 で、cognitoError に理由が入る。
   * 台帳には入っているのに「保存に失敗」と出すのを避けるため。
   */
  saveUser: (
    u: Omit<User, "createdAt" | "updatedAt" | "cognitoStatus" | "cognitoEnabled" | "googleLinked">,
    createCognito = false,
  ) =>
    req<SaveUserResult>("/console/users", {
      method: "POST",
      body: JSON.stringify({ ...u, createCognito }),
    }),

  /** 台帳にいる人の Cognito アカウントを後から作る（招待メールが飛ぶ）。 */
  createCognitoUser: (email: string) =>
    req<{ created: boolean; alreadyExists?: boolean }>(
      `/console/users/${encodeURIComponent(email)}/cognito`,
      { method: "POST" },
    ),

  /**
   * 招待メールを再送する。
   *
   * **仮パスワードが再発行され、失効までの期間もリセットされる。**
   * 前のメールに載っていた値は使えなくなる。
   */
  resendInvite: (email: string) =>
    req<{ resent: boolean }>(`/console/users/${encodeURIComponent(email)}/cognito/resend`, {
      method: "POST",
    }),

  /**
   * Cognito アカウントの有効・無効。
   *
   * 台帳の suspended とは別物。台帳の停止は各アプリの認可を拒否するだけで
   * Cognito のログイン自体は通る。即座に止めたいときはこちら。
   */
  setCognitoEnabled: (email: string, enabled: boolean) =>
    req<{ enabled: boolean }>(`/console/users/${encodeURIComponent(email)}/cognito/enabled`, {
      method: "POST",
      body: JSON.stringify({ enabled }),
    }),
  deleteUser: (email: string) =>
    req<void>(`/console/users/${encodeURIComponent(email)}`, { method: "DELETE" }),

  listGrants: (email: string) =>
    req<{ grants: Grant[] }>(`/console/users/${encodeURIComponent(email)}/grants`),
  saveGrant: (g: Pick<Grant, "email" | "app" | "role" | "scopes">) =>
    req<Grant>(`/console/users/${encodeURIComponent(g.email)}/grants`, {
      method: "POST",
      body: JSON.stringify(g),
    }),
  deleteGrant: (email: string, app: AppKey) =>
    req<void>(`/console/users/${encodeURIComponent(email)}/grants/${app}`, {
      method: "DELETE",
    }),

  /**
   * アプリごとのスコープ候補。
   *
   * 中央はスコープの意味を知らないので、候補は各アプリに聞く。
   * アプリが応答しない場合は空配列を返し、画面は「候補を取得できません」と出す
   * （設定作業そのものは続けられるようにする）。
   *
   * email は「誰の候補か」。**アプリによって候補が人ごとに変わる**ため渡す
   * （task-scope のスペースは共有だが、money-pilot の事業は個人に属する）。
   */
  scopeCandidates: (app: AppKey, email: string) =>
    req<{ candidates: ScopeCandidate[] }>(
      `/console/apps/${app}/scope-candidates?email=${encodeURIComponent(email)}`,
    ),
};
