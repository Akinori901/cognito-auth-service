/**
 * Amplify（Cognito）設定。auth-user-pool の auth-console-web-client を使う。
 *
 * このクライアントは **Google ログインのみ**許可されている（Terraform 側で
 * supported_identity_providers から COGNITO を外している）。メール＋パスワードの
 * 入力欄は出さない。
 *
 * main.tsx から副作用 import して Amplify.configure() を実行する。
 */
import { Amplify } from "aws-amplify";

const userPoolId = import.meta.env.VITE_COGNITO_USER_POOL_ID;
const userPoolClientId = import.meta.env.VITE_COGNITO_CLIENT_ID;
const domainPrefix = import.meta.env.VITE_COGNITO_DOMAIN_PREFIX ?? "auth";
const region = import.meta.env.VITE_COGNITO_REGION ?? "ap-northeast-1";

export const isCognitoConfigured = Boolean(userPoolId && userPoolClientId);

if (isCognitoConfigured) {
  Amplify.configure({
    Auth: {
      Cognito: {
        userPoolId,
        userPoolClientId,
        loginWith: {
          oauth: {
            domain: `${domainPrefix}.auth.${region}.amazoncognito.com`,
            scopes: ["openid", "profile", "email"],
            redirectSignIn: [window.location.origin],
            redirectSignOut: [window.location.origin],
            responseType: "code",
          },
        },
      },
    },
  });
}
