import { useState } from "react";

import { AuthGate } from "@/auth/AuthGate";
import { UserDetail } from "@/pages/UserDetail";
import { UserList } from "@/pages/UserList";
import { AppClientList } from "@/pages/AppClientList";
import { MyApps } from "@/pages/MyApps";

export function App() {
  // 画面が 2 つだけなので router を入れず、状態で切り替える。
  // null = 一覧、"" = 新規登録、それ以外 = そのユーザーの詳細。
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <AuthGate>
      {(email) => (
        <div className="container">
          {selected === null ? (
            <>
              {/* 自分が使えるアプリ。招待された人が最初に見る導線なので
                  ユーザー管理より上に置く。 */}
              <MyApps email={email} />
              <UserList onSelect={setSelected} />
              <AppClientList />
            </>
          ) : (
            <UserDetail email={selected} onBack={() => setSelected(null)} />
          )}
        </div>
      )}
    </AuthGate>
  );
}
