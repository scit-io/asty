import { useCallback, useEffect, useState } from 'react';
import { apiCall, BASE } from './api';
import AuthTab from './tabs/Auth';
import CrudTab from './tabs/Crud';
import WsTab from './tabs/Ws';

type TabKey = 'auth' | 'crud' | 'ws';
type AuthState = { authed: boolean; sub?: string } | null;

export default function App() {
  const [tab, setTab] = useState<TabKey>('auth');
  const [health, setHealth] = useState<string>('…');
  const [auth, setAuth] = useState<AuthState>(null);

  const checkHealth = useCallback(async () => {
    const r = await apiCall<{ status: string; nats: string }>('/health');
    setHealth(r.ok ? `ok (nats: ${r.data?.nats ?? '?'})` : `down (${r.status})`);
  }, []);

  const checkAuth = useCallback(async () => {
    const r = await apiCall<{ sub: string; exp: number; iat: number }>('/api/v1/xauth/me', {
      method: 'POST',
    });
    setAuth(r.ok ? { authed: true, sub: r.data?.sub } : { authed: false });
  }, []);

  // Event-driven, no setInterval: probe on mount, then re-probe when
  // the tab becomes visible or the network comes back. Auth also
  // re-fires from AuthTab via onAuthChanged after every user action.
  useEffect(() => {
    checkHealth();
    checkAuth();
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        checkHealth();
        checkAuth();
      }
    };
    const onOnline = () => {
      checkHealth();
      checkAuth();
    };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('online', onOnline);
    return () => {
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('online', onOnline);
    };
  }, [checkHealth, checkAuth]);

  const authLabel =
    auth === null ? '…' : auth.authed ? `signed in as ${auth.sub ?? '?'}` : 'not signed in';

  return (
    <div className="app">
      <header>
        <h1>platform.go — x-services tester</h1>
        <div className="meta">
          <span>Gateway: <code>{BASE || '(proxy)'}</code></span>
          <span>Health: <code>{health}</code></span>
          <span>Auth: <code>{authLabel}</code></span>
        </div>
        <nav>
          <button className={tab === 'auth' ? 'active' : ''} onClick={() => setTab('auth')}>xauth</button>
          <button className={tab === 'crud' ? 'active' : ''} onClick={() => setTab('crud')}>xhttp</button>
          <button className={tab === 'ws' ? 'active' : ''} onClick={() => setTab('ws')}>xws</button>
        </nav>
      </header>
      <main>
        {tab === 'auth' && <AuthTab onAuthChanged={checkAuth} />}
        {tab === 'crud' && <CrudTab />}
        {tab === 'ws' && <WsTab />}
      </main>
    </div>
  );
}
