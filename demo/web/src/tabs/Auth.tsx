import { useState } from 'react';
import { apiCall } from '../api';

type Props = { onAuthChanged: () => void };

export default function AuthTab({ onAuthChanged }: Props) {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('password');
  const [out, setOut] = useState<string>('');
  const [busy, setBusy] = useState(false);

  const run = async (label: string, fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      const r = await fn();
      setOut(`[${label}] ${JSON.stringify(r, null, 2)}`);
    } catch (e) {
      setOut(`[${label}] error: ${String(e)}`);
    } finally {
      setBusy(false);
      onAuthChanged();
    }
  };

  const login = () =>
    run('login', () =>
      apiCall('/api/v1/xauth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      }),
    );

  const me = () => run('me', () => apiCall('/api/v1/xauth/me', { method: 'POST' }));
  const refresh = () => run('refresh', () => apiCall('/api/v1/xauth/refresh', { method: 'POST' }));
  const logout = () => run('logout', () => apiCall('/api/v1/xauth/logout', { method: 'POST' }));

  return (
    <section>
      <h2>xauth — JWT in HttpOnly cookies</h2>
      <div className="row">
        <input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="username" />
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="password"
        />
      </div>
      <div className="row">
        <button className="primary" disabled={busy} onClick={login}>login</button>
        <button className="secondary" disabled={busy} onClick={me}>me</button>
        <button className="secondary" disabled={busy} onClick={refresh}>refresh</button>
        <button className="secondary" disabled={busy} onClick={logout}>logout</button>
      </div>
      <pre className="out">{out || 'Press a button — the response appears here.'}</pre>
    </section>
  );
}
