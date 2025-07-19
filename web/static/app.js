class MimicUI {
    constructor() {
        this.ws = null;
        this.events = [];
        this.maxEvents = 1000;
        this.autoScroll = true;
        this.currentSession = null;
        
        this.init();
    }

    init() {
        this.setupWebSocket();
        this.setupEventListeners();
        this.loadSessions();
        this.loadInteractions();
    }

    setupWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;
        
        this.ws = new WebSocket(wsUrl);
        
        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.updateConnectionStatus(true);
        };
        
        this.ws.onmessage = (event) => {
            try {
                const message = JSON.parse(event.data);
                this.handleMessage(message);
            } catch (error) {
                console.error('Failed to parse WebSocket message:', error);
            }
        };
        
        this.ws.onclose = () => {
            console.log('WebSocket disconnected');
            this.updateConnectionStatus(false);
            // Attempt to reconnect after 3 seconds
            setTimeout(() => this.setupWebSocket(), 3000);
        };
        
        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.updateConnectionStatus(false);
        };
    }

    updateConnectionStatus(connected) {
        const statusEl = document.getElementById('connection-status');
        if (connected) {
            statusEl.textContent = 'Connected';
            statusEl.className = 'status-connected';
        } else {
            statusEl.textContent = 'Disconnected';
            statusEl.className = 'status-disconnected';
        }
    }

    handleMessage(message) {
        switch (message.type) {
            case 'request':
            case 'response':
                this.addEvent(message);
                break;
            default:
                console.log('Unknown message type:', message.type);
        }
    }

    addEvent(message) {
        this.events.unshift({
            id: Date.now() + Math.random(),
            timestamp: new Date(message.timestamp),
            ...message.data
        });

        // Limit the number of events
        if (this.events.length > this.maxEvents) {
            this.events = this.events.slice(0, this.maxEvents);
        }

        this.updateEventsList();
        this.updateEventCount();

        // Auto-scroll if enabled
        if (this.autoScroll) {
            const eventsList = document.getElementById('events-list');
            eventsList.scrollTop = 0;
        }
    }

    updateEventsList() {
        const eventsList = document.getElementById('events-list');
        
        if (this.events.length === 0) {
            eventsList.innerHTML = '<div class="no-events">No events yet. Start making requests to see them here.</div>';
            return;
        }

        const eventsHtml = this.events.map(event => this.renderEvent(event)).join('');
        eventsList.innerHTML = eventsHtml;
    }

    renderEvent(event) {
        const timestamp = event.timestamp.toLocaleTimeString();
        const statusClass = event.status ? `status-${Math.floor(event.status / 100)}xx` : '';
        const methodClass = `method-${event.method}`;
        
        let bodyHtml = '';
        if (event.body && event.body.trim()) {
            const displayBody = event.body.length > 200 ? 
                event.body.substring(0, 200) + '...' : event.body;
            bodyHtml = `<div class="event-body">${this.escapeHtml(displayBody)}</div>`;
        }

        return `
            <div class="event-item">
                <div class="event-header">
                    <div>
                        <span class="event-method ${methodClass}">${event.method}</span>
                        <span class="event-endpoint">${event.endpoint}</span>
                        ${event.status ? `<span class="event-status ${statusClass}">${event.status}</span>` : ''}
                    </div>
                    <div class="event-timestamp">${timestamp}</div>
                </div>
                <div class="event-meta">
                    <span>Session: ${event.session_name}</span>
                    <span>From: ${event.remote_addr}</span>
                    <span>Type: ${event.type}</span>
                    ${event.request_id ? `<span>ID: ${event.request_id.substring(0, 8)}...</span>` : ''}
                </div>
                ${bodyHtml}
            </div>
        `;
    }

    updateEventCount() {
        document.getElementById('event-count').textContent = this.events.length;
    }

    setupEventListeners() {
        // Tab switching
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                this.switchTab(e.target.dataset.tab);
            });
        });

        // Auto-scroll checkbox
        document.getElementById('auto-scroll').addEventListener('change', (e) => {
            this.autoScroll = e.target.checked;
        });

        // Clear events button
        document.getElementById('clear-events').addEventListener('click', () => {
            this.events = [];
            this.updateEventsList();
            this.updateEventCount();
        });

        // Refresh sessions button
        document.getElementById('refresh-sessions').addEventListener('click', () => {
            this.loadSessions();
        });

        // Clear all button
        document.getElementById('clear-all').addEventListener('click', () => {
            if (confirm('Are you sure you want to clear all sessions and data?')) {
                this.clearAll();
            }
        });

        // Session filter
        document.getElementById('session-filter').addEventListener('change', (e) => {
            this.filterInteractions(e.target.value);
        });

        // Modal close
        document.querySelector('.close').addEventListener('click', () => {
            document.getElementById('interaction-modal').style.display = 'none';
        });

        // Click outside modal to close
        window.addEventListener('click', (e) => {
            const modal = document.getElementById('interaction-modal');
            if (e.target === modal) {
                modal.style.display = 'none';
            }
        });
    }

    switchTab(tabName) {
        // Update tab buttons
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.classList.remove('active');
        });
        document.querySelector(`[data-tab="${tabName}"]`).classList.add('active');

        // Update tab content
        document.querySelectorAll('.tab-content').forEach(content => {
            content.classList.remove('active');
        });
        document.getElementById(`${tabName}-tab`).classList.add('active');

        // Load data if needed
        if (tabName === 'interactions') {
            this.loadInteractions();
        }
    }

    async loadSessions() {
        try {
            const response = await fetch('/api/sessions');
            const sessions = await response.json();
            this.renderSessions(sessions);
            this.updateSessionFilter(sessions);
        } catch (error) {
            console.error('Failed to load sessions:', error);
        }
    }

    renderSessions(sessions) {
        const sessionsList = document.getElementById('sessions-list');
        
        if (sessions.length === 0) {
            sessionsList.innerHTML = '<div style="padding: 20px; text-align: center; color: #7f8c8d;">No sessions found</div>';
            return;
        }

        const sessionsHtml = sessions.map(session => {
            const createdAt = new Date(session.created_at).toLocaleString();
            return `
                <div class="session-item" data-session-id="${session.id}">
                    <div class="session-name">${session.session_name}</div>
                    <div class="session-meta">
                        Created: ${createdAt}<br>
                        ${session.description || 'No description'}
                    </div>
                </div>
            `;
        }).join('');
        
        sessionsList.innerHTML = sessionsHtml;

        // Add click listeners
        sessionsList.querySelectorAll('.session-item').forEach(item => {
            item.addEventListener('click', () => {
                this.selectSession(parseInt(item.dataset.sessionId));
            });
        });
    }

    updateSessionFilter(sessions) {
        const filter = document.getElementById('session-filter');
        const currentValue = filter.value;
        
        filter.innerHTML = '<option value="">All Sessions</option>';
        sessions.forEach(session => {
            const option = document.createElement('option');
            option.value = session.id;
            option.textContent = session.session_name;
            if (currentValue == session.id) option.selected = true;
            filter.appendChild(option);
        });
    }

    selectSession(sessionId) {
        this.currentSession = sessionId;
        
        // Update UI
        document.querySelectorAll('.session-item').forEach(item => {
            item.classList.remove('active');
        });
        document.querySelector(`[data-session-id="${sessionId}"]`).classList.add('active');

        // Load interactions for this session
        this.loadSessionInteractions(sessionId);
    }

    async loadSessionInteractions(sessionId) {
        try {
            const response = await fetch(`/api/sessions/${sessionId}`);
            const interactions = await response.json();
            console.log(`Loaded ${interactions.length} interactions for session ${sessionId}`);
        } catch (error) {
            console.error('Failed to load session interactions:', error);
        }
    }

    async loadInteractions() {
        try {
            const response = await fetch('/api/interactions/');
            const interactions = await response.json();
            this.renderInteractions(interactions);
        } catch (error) {
            console.error('Failed to load interactions:', error);
        }
    }

    renderInteractions(interactions) {
        const interactionsList = document.getElementById('interactions-list');
        
        if (interactions.length === 0) {
            interactionsList.innerHTML = '<div style="padding: 40px; text-align: center; color: #7f8c8d;">No interactions found</div>';
            return;
        }

        // Sort by timestamp (newest first)
        interactions.sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));

        const interactionsHtml = interactions.map(interaction => {
            const timestamp = new Date(interaction.timestamp).toLocaleString();
            const statusClass = `status-${Math.floor(interaction.response_status / 100)}xx`;
            const methodClass = `method-${interaction.method}`;

            return `
                <div class="interaction-item" data-interaction-id="${interaction.id}">
                    <div class="interaction-header">
                        <div>
                            <span class="event-method ${methodClass}">${interaction.method}</span>
                            <span class="interaction-endpoint">${interaction.endpoint}</span>
                            <span class="event-status ${statusClass}">${interaction.response_status}</span>
                        </div>
                        <div class="interaction-time">${timestamp}</div>
                    </div>
                    <div class="event-meta">
                        <span>Sequence: ${interaction.sequence_number}</span>
                        <span>Protocol: ${interaction.protocol}</span>
                        <span>ID: ${interaction.request_id.substring(0, 8)}...</span>
                    </div>
                </div>
            `;
        }).join('');
        
        interactionsList.innerHTML = interactionsHtml;

        // Add click listeners
        interactionsList.querySelectorAll('.interaction-item').forEach(item => {
            item.addEventListener('click', () => {
                const interaction = interactions.find(i => i.id == item.dataset.interactionId);
                this.showInteractionDetail(interaction);
            });
        });
    }

    filterInteractions(sessionId) {
        // This is a simple implementation - in a real app you might want to 
        // filter on the server side for better performance
        this.loadInteractions();
    }

    showInteractionDetail(interaction) {
        const modal = document.getElementById('interaction-modal');
        const detail = document.getElementById('interaction-detail');
        
        let requestHeaders = {};
        let responseHeaders = {};
        
        try {
            requestHeaders = JSON.parse(interaction.request_headers || '{}');
            responseHeaders = JSON.parse(interaction.response_headers || '{}');
        } catch (error) {
            console.error('Failed to parse headers:', error);
        }

        const requestBody = interaction.request_body ? 
            new TextDecoder().decode(new Uint8Array(interaction.request_body)) : '';
        const responseBody = interaction.response_body ? 
            new TextDecoder().decode(new Uint8Array(interaction.response_body)) : '';

        detail.innerHTML = `
            <h2>${interaction.method} ${interaction.endpoint}</h2>
            <p><strong>Request ID:</strong> ${interaction.request_id}</p>
            <p><strong>Timestamp:</strong> ${new Date(interaction.timestamp).toLocaleString()}</p>
            <p><strong>Sequence:</strong> ${interaction.sequence_number}</p>
            <p><strong>Status:</strong> ${interaction.response_status}</p>
            
            <div class="detail-section">
                <h4>Request Headers</h4>
                <div class="detail-content">${JSON.stringify(requestHeaders, null, 2)}</div>
            </div>
            
            <div class="detail-section">
                <h4>Request Body</h4>
                <div class="detail-content">${this.escapeHtml(requestBody) || '(empty)'}</div>
            </div>
            
            <div class="detail-section">
                <h4>Response Headers</h4>
                <div class="detail-content">${JSON.stringify(responseHeaders, null, 2)}</div>
            </div>
            
            <div class="detail-section">
                <h4>Response Body</h4>
                <div class="detail-content">${this.escapeHtml(responseBody) || '(empty)'}</div>
            </div>
        `;
        
        modal.style.display = 'block';
    }

    async clearAll() {
        try {
            const response = await fetch('/api/clear', { method: 'POST' });
            if (response.ok) {
                this.loadSessions();
                this.loadInteractions();
                this.events = [];
                this.updateEventsList();
                this.updateEventCount();
            }
        } catch (error) {
            console.error('Failed to clear all:', error);
        }
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

// Initialize the UI when the page loads
document.addEventListener('DOMContentLoaded', () => {
    new MimicUI();
});