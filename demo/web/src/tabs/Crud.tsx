import { useState } from 'react';
import { apiCall } from '../api';

type Item = {
  id: number;
  name: string;
  value: string;
  created_at: string;
  updated_at: string;
};

export default function CrudTab() {
  const [items, setItems] = useState<Item[]>([]);
  const [createName, setCreateName] = useState('');
  const [createValue, setCreateValue] = useState('');
  const [getId, setGetId] = useState('');
  const [updateId, setUpdateId] = useState('');
  const [updateName, setUpdateName] = useState('');
  const [updateValue, setUpdateValue] = useState('');
  const [deleteId, setDeleteId] = useState('');
  const [out, setOut] = useState<string>('');
  const [busy, setBusy] = useState(false);

  const show = (label: string, r: unknown) => setOut(`[${label}] ${JSON.stringify(r, null, 2)}`);

  const list = async () => {
    setBusy(true);
    const r = await apiCall<Item[]>('/api/v1/xhttp/list', { method: 'POST', body: '{}' });
    if (r.ok && Array.isArray(r.data)) setItems(r.data);
    show('list', r);
    setBusy(false);
  };

  const create = async () => {
    setBusy(true);
    const r = await apiCall<Item>('/api/v1/xhttp/create', {
      method: 'POST',
      body: JSON.stringify({ name: createName, value: createValue }),
    });
    show('create', r);
    if (r.ok) {
      setCreateName('');
      setCreateValue('');
      await list();
    }
    setBusy(false);
  };

  const get = async () => {
    setBusy(true);
    const r = await apiCall<Item>('/api/v1/xhttp/get', {
      method: 'POST',
      body: JSON.stringify({ id: Number(getId) }),
    });
    show('get', r);
    setBusy(false);
  };

  const update = async () => {
    setBusy(true);
    const r = await apiCall<Item>('/api/v1/xhttp/update', {
      method: 'POST',
      body: JSON.stringify({ id: Number(updateId), name: updateName, value: updateValue }),
    });
    show('update', r);
    if (r.ok) await list();
    setBusy(false);
  };

  const del = async () => {
    setBusy(true);
    const r = await apiCall('/api/v1/xhttp/delete', {
      method: 'POST',
      body: JSON.stringify({ id: Number(deleteId) }),
    });
    show('delete', r);
    if (r.ok) {
      setDeleteId('');
      await list();
    }
    setBusy(false);
  };

  return (
    <section>
      <h2>xhttp — CRUD</h2>

      <table className="handles">
        <thead>
          <tr>
            <th style={{ width: 90 }}>Endpoint</th>
            <th>Inputs</th>
            <th style={{ width: 70 }}>Auth</th>
            <th style={{ width: 110 }}>Action</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>create</code></td>
            <td>
              <div className="row" style={{ margin: 0 }}>
                <input placeholder="name" value={createName} onChange={(e) => setCreateName(e.target.value)} />
                <input placeholder="value" value={createValue} onChange={(e) => setCreateValue(e.target.value)} />
              </div>
            </td>
            <td><code>required</code></td>
            <td>
              <button
                className="primary"
                disabled={busy || !createName}
                onClick={create}
              >
                create
              </button>
            </td>
          </tr>
          <tr>
            <td><code>list</code></td>
            <td />
            <td><code>optional</code></td>
            <td>
              <button className="secondary" disabled={busy} onClick={list}>list</button>
            </td>
          </tr>
          <tr>
            <td><code>get</code></td>
            <td>
              <input
                placeholder="id"
                value={getId}
                onChange={(e) => setGetId(e.target.value)}
                style={{ width: 80 }}
              />
            </td>
            <td><code>optional</code></td>
            <td>
              <button className="secondary" disabled={busy || !getId} onClick={get}>get</button>
            </td>
          </tr>
          <tr>
            <td><code>update</code></td>
            <td>
              <div className="row" style={{ margin: 0 }}>
                <input
                  placeholder="id"
                  value={updateId}
                  onChange={(e) => setUpdateId(e.target.value)}
                  style={{ width: 80 }}
                />
                <input placeholder="name" value={updateName} onChange={(e) => setUpdateName(e.target.value)} />
                <input placeholder="value" value={updateValue} onChange={(e) => setUpdateValue(e.target.value)} />
              </div>
            </td>
            <td><code>required</code></td>
            <td>
              <button
                className="secondary"
                disabled={busy || !updateId || !updateName}
                onClick={update}
              >
                update
              </button>
            </td>
          </tr>
          <tr>
            <td><code>delete</code></td>
            <td>
              <input
                placeholder="id"
                value={deleteId}
                onChange={(e) => setDeleteId(e.target.value)}
                style={{ width: 80 }}
              />
            </td>
            <td><code>required</code></td>
            <td>
              <button
                className="secondary"
                disabled={busy || !deleteId}
                onClick={del}
              >
                delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      {items.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>id</th>
              <th>name</th>
              <th>value</th>
              <th>updated</th>
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.id}>
                <td>{it.id}</td>
                <td>{it.name}</td>
                <td>{it.value}</td>
                <td>{new Date(it.updated_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <pre className="out">{out || 'Press an action — the response appears here.'}</pre>
    </section>
  );
}
