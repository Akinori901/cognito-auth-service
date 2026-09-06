"""Cognito Pre-Sign-Up トリガー（auth-user-pool 共通）。

招待制ゲート: **admin が Cognito に事前作成した email のみ** サインアップ（Google 連携含む）を許可する。
未招待の Google アカウントは拒否する。

動作:
- Google などの外部 IdP 経由の新規サインアップ（PreSignUp_ExternalProvider）:
    - 同じ email の「事前作成済みネイティブユーザー」が存在すれば → 外部IdをリンクしてOK
    - 存在しなければ → 例外を投げて拒否（未招待）
- ネイティブのセルフサインアップ（PreSignUp_SignUp）:
    - このプールは admin 招待のみ運用なのでセルフサインアップは想定外 → 拒否

参考: 標準的な "invite-only federation" パターン。
"""

import os

import boto3

cognito = boto3.client("cognito-idp")


def _find_native_user(user_pool_id: str, email: str):
    """同じ email のネイティブ（外部IdPでない）ユーザーを探す。無ければ None。"""
    resp = cognito.list_users(
        UserPoolId=user_pool_id,
        Filter=f'email = "{email}"',
        Limit=10,
    )
    for u in resp.get("Users", []):
        username = u.get("Username", "")
        # 外部IdP由来のユーザー名は "Google_..." のようにプレフィックスが付く
        if not username.startswith(("Google_", "Facebook_", "SignInWithApple_", "LoginWithAmazon_")):
            return u
    return None


def lambda_handler(event, context):
    trigger = event.get("triggerSource", "")
    user_pool_id = event["userPoolId"]
    attrs = event["request"]["userAttributes"]
    email = (attrs.get("email") or "").lower()

    if trigger == "PreSignUp_ExternalProvider":
        # Google 連携の新規サインアップ。admin 事前作成ユーザーがあれば許可＋自動リンク。
        native = _find_native_user(user_pool_id, email)
        if native is None:
            raise Exception("このメールアドレスは招待されていません。管理者にお問い合わせください。")

        # 外部IdをネイティブユーザーにリンクしてSSOを成立させる
        # event.userName は "Google_<sub>" 形式
        provider_user = event["userName"]
        if "_" in provider_user:
            provider_name, provider_sub = provider_user.split("_", 1)
            try:
                cognito.admin_link_provider_for_user(
                    UserPoolId=user_pool_id,
                    DestinationUser={"ProviderName": "Cognito", "ProviderAttributeValue": native["Username"]},
                    SourceUser={
                        "ProviderName": provider_name,
                        "ProviderAttributeName": "Cognito_Subject",
                        "ProviderAttributeValue": provider_sub,
                    },
                )
            except cognito.exceptions.InvalidParameterException:
                # 既にリンク済みなどは無視
                pass

        # 外部IdPユーザーは自動確認・メール確認済みにする
        event["response"]["autoConfirmUser"] = True
        event["response"]["autoVerifyEmail"] = True
        return event

    if trigger == "PreSignUp_SignUp":
        # セルフサインアップは許可しない（admin 招待のみ運用）
        raise Exception("セルフサインアップは無効です。管理者による招待が必要です。")

    # 管理者作成（PreSignUp_AdminCreateUser）はそのまま通す
    return event
