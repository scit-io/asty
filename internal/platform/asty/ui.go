package asty

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Asty Dashboard</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f5f5f5;
            color: #333;
        }

        header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }

        header h1 {
            font-size: 28px;
            font-weight: 600;
        }

        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
        }

        .status-bar {
            background: white;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
        }

        .status-item {
            text-align: center;
        }

        .status-item .value {
            font-size: 32px;
            font-weight: bold;
            color: #667eea;
        }

        .status-item .label {
            color: #666;
            font-size: 14px;
            margin-top: 5px;
        }

        .section {
            background: white;
            border-radius: 8px;
            padding: 20px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }

        .section h2 {
            font-size: 20px;
            margin-bottom: 15px;
            color: #667eea;
        }

        table {
            width: 100%;
            border-collapse: collapse;
        }

        th {
            text-align: left;
            padding: 12px;
            background: #f8f9fa;
            font-weight: 600;
            border-bottom: 2px solid #e0e0e0;
        }

        td {
            padding: 12px;
            border-bottom: 1px solid #e0e0e0;
        }

        .badge {
            display: inline-block;
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 600;
        }

        .badge-success {
            background: #d4edda;
            color: #155724;
        }

        .badge-warning {
            background: #fff3cd;
            color: #856404;
        }

        .badge-danger {
            background: #f8d7da;
            color: #721c24;
        }

        .badge-info {
            background: #d1ecf1;
            color: #0c5460;
        }

        .loading {
            text-align: center;
            padding: 40px;
            color: #999;
        }

        .error {
            background: #f8d7da;
            color: #721c24;
            padding: 15px;
            border-radius: 4px;
            margin-bottom: 20px;
        }

        .refresh-btn {
            background: #667eea;
            color: white;
            border: none;
            padding: 8px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 14px;
        }

        .refresh-btn:hover {
            background: #5568d3;
        }

        .auto-refresh {
            float: right;
            color: #666;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <header>
        <div class="container">
            <h1>Asty Dashboard</h1>
        </div>
    </header>

    <div class="container">
        <div class="status-bar" id="status-bar">
            <div class="status-item">
                <div class="value" id="nodes-total">-</div>
                <div class="label">Total Nodes</div>
            </div>
            <div class="status-item">
                <div class="value" id="nodes-healthy">-</div>
                <div class="label">Healthy Nodes</div>
            </div>
            <div class="status-item">
                <div class="value" id="services-count">-</div>
                <div class="label">Services</div>
            </div>
            <div class="status-item">
                <div class="value" id="leader-status">-</div>
                <div class="label">Leader Status</div>
            </div>
        </div>

        <div class="section">
            <h2>
                Cluster Nodes
                <button class="refresh-btn" onclick="loadData()">Refresh</button>
                <span class="auto-refresh">Auto-refresh: 10s</span>
            </h2>
            <div id="nodes-loading" class="loading">Loading...</div>
            <div id="nodes-error" class="error" style="display: none;"></div>
            <table id="nodes-table" style="display: none;">
                <thead>
                    <tr>
                        <th>Node ID</th>
                        <th>Datacenter</th>
                        <th>Status</th>
                        <th>CPU</th>
                        <th>Memory</th>
                        <th>Processes</th>
                        <th>Last Seen</th>
                    </tr>
                </thead>
                <tbody id="nodes-tbody"></tbody>
            </table>
        </div>

        <div class="section">
            <h2>Services</h2>
            <div id="services-loading" class="loading">Loading...</div>
            <div id="services-error" class="error" style="display: none;"></div>
            <table id="services-table" style="display: none;">
                <thead>
                    <tr>
                        <th>Service</th>
                        <th>Type</th>
                        <th>CPU</th>
                        <th>Memory</th>
                    </tr>
                </thead>
                <tbody id="services-tbody"></tbody>
            </table>
        </div>
    </div>

    <script>
        async function loadData() {
            try {
                await Promise.all([
                    loadStatus(),
                    loadNodes(),
                    loadServices()
                ]);
            } catch (err) {
                console.error('Failed to load data:', err);
            }
        }

        async function loadStatus() {
            const resp = await fetch('/api/v1/status');
            const data = await resp.json();

            document.getElementById('nodes-total').textContent = data.cluster.nodes_total;
            document.getElementById('nodes-healthy').textContent = data.cluster.nodes_healthy;
            document.getElementById('services-count').textContent = data.services.loaded;
            document.getElementById('leader-status').textContent = data.cluster.is_leader ? 'Leader' : 'Follower';
        }

        async function loadNodes() {
            try {
                document.getElementById('nodes-loading').style.display = 'block';
                document.getElementById('nodes-error').style.display = 'none';

                const resp = await fetch('/api/v1/nodes');
                const data = await resp.json();

                const tbody = document.getElementById('nodes-tbody');
                tbody.innerHTML = '';

                data.nodes.forEach(node => {
                    const row = tbody.insertRow();

                    const statusClass = node.status === 'ready' ? 'badge-success' : 'badge-warning';
                    const lastSeen = new Date(node.last_seen).toLocaleString();

                    row.innerHTML = '<td>' + node.id + '</td>' +
                        '<td>' + node.datacenter + '</td>' +
                        '<td><span class="badge ' + statusClass + '">' + node.status + '</span></td>' +
                        '<td>' + node.cpu_available + ' / ' + node.cpu_total + ' MHz</td>' +
                        '<td>' + node.memory_available + ' / ' + node.memory_total + ' MB</td>' +
                        '<td>' + (node.processes ? node.processes.length : 0) + '</td>' +
                        '<td>' + lastSeen + '</td>';
                });

                document.getElementById('nodes-loading').style.display = 'none';
                document.getElementById('nodes-table').style.display = 'table';
            } catch (err) {
                document.getElementById('nodes-loading').style.display = 'none';
                document.getElementById('nodes-error').textContent = 'Failed to load nodes: ' + err.message;
                document.getElementById('nodes-error').style.display = 'block';
            }
        }

        async function loadServices() {
            try {
                document.getElementById('services-loading').style.display = 'block';
                document.getElementById('services-error').style.display = 'none';

                const resp = await fetch('/api/v1/services');
                const data = await resp.json();

                const tbody = document.getElementById('services-tbody');
                tbody.innerHTML = '';

                data.services.forEach(svc => {
                    const row = tbody.insertRow();
                    const typeClass = svc.type === 'system' ? 'badge-info' : 'badge-success';

                    row.innerHTML = '<td>' + svc.name + '</td>' +
                        '<td><span class="badge ' + typeClass + '">' + svc.type + '</span></td>' +
                        '<td>' + svc.resources.cpu + ' MHz</td>' +
                        '<td>' + svc.resources.memory + ' MB</td>';
                });

                document.getElementById('services-loading').style.display = 'none';
                document.getElementById('services-table').style.display = 'table';
            } catch (err) {
                document.getElementById('services-loading').style.display = 'none';
                document.getElementById('services-error').textContent = 'Failed to load services: ' + err.message;
                document.getElementById('services-error').style.display = 'block';
            }
        }

        // Initial load
        loadData();

        // Auto-refresh every 10 seconds
        setInterval(loadData, 10000);
    </script>
</body>
</html>
`
