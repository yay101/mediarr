// Mediarr Alpine.js Application
// Global stores and initialization

document.addEventListener('alpine:init', () => {
    // Notification store - toast messages
    Alpine.store('notifications', {
        items: [],
        counter: 0,
        
        add(message, level = 'info') {
            const id = ++this.counter;
            this.items.push({ id, message, level, timestamp: Date.now() });
            
            // Auto-remove after 5 seconds
            setTimeout(() => this.remove(id), 5000);
        },
        
        remove(id) {
            this.items = this.items.filter(n => n.id !== id);
        },
        
        success(message) { this.add(message, 'success'); },
        error(message) { this.add(message, 'error'); },
        warning(message) { this.add(message, 'warning'); },
        info(message) { this.add(message, 'info'); }
    });

    // User store - authentication state
    Alpine.store('user', {
        id: null,
        username: '',
        role: '',
        authenticated: false,
        
        async fetch() {
            try {
                const res = await fetch('/api/v1/auth/me');
                const data = await res.json();
                if (data.authenticated) {
                    this.id = data.user.id;
                    this.username = data.user.username;
                    this.role = data.user.role;
                    this.authenticated = true;
                }
            } catch (e) {
                console.error('Failed to fetch user:', e);
            }
        },
        
        logout() {
            fetch('/api/v1/auth/logout', { method: 'POST' });
            this.id = null;
            this.username = '';
            this.role = '';
            this.authenticated = false;
            window.location.href = '/login';
        }
    });

    // WebSocket store - connection state
    Alpine.store('ws', {
        conn: null,
        connected: false,
        subscriptions: new Set(),
        messageHandlers: [],
        
        connect() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = `${protocol}//${window.location.host}/ws`;
            
            this.conn = new WebSocket(wsUrl);
            
            this.conn.onopen = () => {
                this.connected = true;
                // Re-subscribe to channels
                this.subscriptions.forEach(channel => {
                    this.send({ type: 'subscribe', channel });
                });
            };
            
            this.conn.onclose = () => {
                this.connected = false;
                // Reconnect after 3 seconds
                setTimeout(() => this.connect(), 3000);
            };
            
            this.conn.onmessage = (event) => {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            };
        },
        
        send(data) {
            if (this.conn && this.conn.readyState === WebSocket.OPEN) {
                this.conn.send(JSON.stringify(data));
            }
        },
        
        subscribe(channel) {
            this.subscriptions.add(channel);
            this.send({ type: 'subscribe', channel });
        },
        
        unsubscribe(channel) {
            this.subscriptions.delete(channel);
            this.send({ type: 'unsubscribe', channel });
        },
        
        onmessage(handler) {
            this.messageHandlers.push(handler);
        },
        
        handleMessage(msg) {
            switch (msg.type) {
                case 'update':
                case 'refresh':
                    // Dispatch to handlers
                    this.messageHandlers.forEach(h => h(msg));
                    // Trigger global refresh if needed
                    if (msg.type === 'refresh' && msg.path) {
                        Alpine.store('notifications').add('Page updated', 'info');
                    }
                    break;
                case 'notification':
                    Alpine.store('notifications').add(msg.message, msg.level || 'info');
                    break;
                case 'progress':
                    this.messageHandlers.forEach(h => h(msg));
                    break;
            }
        }
    });

    // UI store - sidebar, modals, etc.
    Alpine.store('ui', {
        sidebarOpen: true,
        activeModal: null,
        
        toggleSidebar() {
            this.sidebarOpen = !this.sidebarOpen;
        }
    });
});

// App initialization
function app() {
    return {
        init() {
            // Fetch user state
            Alpine.store('user').fetch();
            
            // Connect WebSocket
            Alpine.store('ws').connect();
        }
    };
}

// Format bytes to human readable
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// Format date/time
function formatDate(dateStr) {
    if (!dateStr) return '';
    const date = new Date(dateStr);
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
}

// Format relative time (e.g., "2 hours ago")
function timeAgo(dateStr) {
    if (!dateStr) return '';
    const date = new Date(dateStr);
    const now = new Date();
    const seconds = Math.floor((now - date) / 1000);
    
    const intervals = {
        year: 31536000,
        month: 2592000,
        week: 604800,
        day: 86400,
        hour: 3600,
        minute: 60
    };
    
    for (const [unit, secondsInUnit] of Object.entries(intervals)) {
        const interval = Math.floor(seconds / secondsInUnit);
        if (interval >= 1) {
            return `${interval} ${unit}${interval > 1 ? 's' : ''} ago`;
        }
    }
    return 'just now';
}

// Escape HTML to prevent XSS
function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}
