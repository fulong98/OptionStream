import json
import tkinter as tk
from tkinter import ttk, messagebox
import threading
import websocket
import time
from datetime import datetime
import yaml
import os

class OrderbookApp:
    def __init__(self, root):
        self.root = root
        self.root.title("Deribit BTC Options Orderbook Viewer")
        self.root.geometry("800x600")
        self.root.minsize(600, 400)
        
        # WebSocket connection
        self.ws = None
        self.connected = False
        self.current_instrument = ""
        self.instruments = self.load_instruments_from_config()
        
        # Create UI
        self.create_ui()
    def load_instruments_from_config(self):
        """Load instruments from config.yaml file"""
        config_paths = ['config.yaml', 'config/config.yaml', './config/config.yaml']
        
        for path in config_paths:
            if os.path.exists(path):
                try:
                    with open(path, 'r') as file:
                        config = yaml.safe_load(file)
                        if config and 'deribit' in config and 'instruments' in config['deribit']:
                            return config['deribit']['instruments']
                except Exception as e:
                    print(f"Error loading config from {path}: {e}")
        
    def create_ui(self):
        # Main frame
        main_frame = ttk.Frame(self.root, padding="10")
        main_frame.pack(fill=tk.BOTH, expand=True)
        
        # Header frame
        header_frame = ttk.Frame(main_frame)
        header_frame.pack(fill=tk.X, pady=(0, 10))
        
        # Instrument selector
        ttk.Label(header_frame, text="Instrument:").pack(side=tk.LEFT, padx=(0, 5))
        self.instrument_var = tk.StringVar()
        self.instrument_combo = ttk.Combobox(header_frame, textvariable=self.instrument_var, 
                                             values=self.instruments, width=30)
        self.instrument_combo.pack(side=tk.LEFT, padx=(0, 20))
        self.instrument_combo.bind("<<ComboboxSelected>>", self.on_instrument_selected)
        
        # Status indicators
        self.status_frame = ttk.Frame(header_frame)
        self.status_frame.pack(side=tk.RIGHT)
        
        ttk.Label(self.status_frame, text="Status:").pack(side=tk.LEFT, padx=(0, 5))
        self.status_label = ttk.Label(self.status_frame, text="Disconnected", foreground="red")
        self.status_label.pack(side=tk.LEFT, padx=(0, 20))
        
        ttk.Label(self.status_frame, text="Updated:").pack(side=tk.LEFT, padx=(0, 5))
        self.last_updated_label = ttk.Label(self.status_frame, text="-")
        self.last_updated_label.pack(side=tk.LEFT, padx=(0, 20))
        
        ttk.Label(self.status_frame, text="Mid:").pack(side=tk.LEFT, padx=(0, 5))
        self.mid_price_label = ttk.Label(self.status_frame, text="-")
        self.mid_price_label.pack(side=tk.LEFT)
        
        # Button frame
        button_frame = ttk.Frame(main_frame)
        button_frame.pack(fill=tk.X, pady=(0, 10))
        
        self.connect_button = ttk.Button(button_frame, text="Connect", command=self.toggle_connection)
        self.connect_button.pack(side=tk.LEFT, padx=(0, 10))
        
        # Orderbook frame
        orderbook_frame = ttk.Frame(main_frame)
        orderbook_frame.pack(fill=tk.BOTH, expand=True)
        
        # Orderbook headers
        headers_frame = ttk.Frame(orderbook_frame)
        headers_frame.pack(fill=tk.X)
        
        ttk.Label(headers_frame, text="Bids", width=20, anchor=tk.CENTER, 
                  background="#e6ffe6").pack(side=tk.LEFT, fill=tk.X, expand=True)
        ttk.Label(headers_frame, text="Price", width=20, anchor=tk.CENTER, 
                  background="#f0f0f0").pack(side=tk.LEFT, fill=tk.X, expand=True)
        ttk.Label(headers_frame, text="Asks", width=20, anchor=tk.CENTER, 
                  background="#ffe6e6").pack(side=tk.LEFT, fill=tk.X, expand=True)
        
        # Orderbook treeview
        self.orderbook_tree = ttk.Treeview(orderbook_frame, columns=("bid", "price", "ask"), 
                                           show="tree headings")
        self.orderbook_tree.pack(fill=tk.BOTH, expand=True)
        
        # Configure treeview columns
        self.orderbook_tree.column("#0", width=0, stretch=tk.NO)
        self.orderbook_tree.column("bid", width=100, anchor=tk.E)
        self.orderbook_tree.column("price", width=100, anchor=tk.CENTER)
        self.orderbook_tree.column("ask", width=100, anchor=tk.W)
        
        # Add scrollbar
        scrollbar = ttk.Scrollbar(orderbook_frame, orient=tk.VERTICAL, command=self.orderbook_tree.yview)
        scrollbar.pack(side=tk.RIGHT, fill=tk.Y)
        self.orderbook_tree.configure(yscrollcommand=scrollbar.set)
        
        # Status bar
        self.status_bar = ttk.Label(main_frame, text="Ready", relief=tk.SUNKEN, anchor=tk.W)
        self.status_bar.pack(fill=tk.X, side=tk.BOTTOM, pady=(10, 0))
        
        # Set the first instrument as default
        if self.instruments:
            self.instrument_var.set(self.instruments[0])
    
    def toggle_connection(self):
        if not self.connected:
            self.connect_to_websocket()
        else:
            self.disconnect_websocket()
    
    def connect_to_websocket(self):
        instrument = self.instrument_var.get()
        if not instrument:
            messagebox.showerror("Error", "Please select an instrument first")
            return
        
        self.status_bar.config(text=f"Connecting to WebSocket for {instrument}...")
        self.current_instrument = instrument
        
        # WebSocket URL - adjust this to your server address
        ws_url = f"ws://localhost:8081/ws?instrument={instrument}"
        
        # Start WebSocket connection in a separate thread
        self.ws_thread = threading.Thread(target=self.websocket_thread, args=(ws_url,))
        self.ws_thread.daemon = True
        self.ws_thread.start()
    
    def disconnect_websocket(self):
        if self.ws:
            self.ws.close()
        self.connected = False
        self.status_label.config(text="Disconnected", foreground="red")
        self.connect_button.config(text="Connect")
        self.clear_orderbook()
        self.status_bar.config(text=f"Disconnected from WebSocket")
    
    def websocket_thread(self, ws_url):
        def on_message(ws, message):
            # Create a function to update UI with the parsed data
            def update_ui_with_data(data):
                # Update the orderbook in the main thread
                self.update_orderbook(data)
            
            # Create a function to update UI with error message
            def update_ui_with_error(error_msg):
                self.status_bar.config(text=error_msg)
            
            try:
                data = json.loads(message)
                
                # Check if this is a status or error message
                if 'status' in data or 'error' in data:
                    status = data.get('status') or data.get('error')
                    self.root.after(0, lambda: self.status_bar.config(text=status))
                    return
                
                # Update the orderbook in the main thread
                self.root.after(0, lambda: update_ui_with_data(data))
            except json.JSONDecodeError as json_error:
                print(f"Error parsing message: {str(json_error)}...")
                error_msg = f"Error parsing message: {str(json_error)[:50]}..."
                self.root.after(0, lambda: update_ui_with_error(error_msg))
            except Exception as general_error:
                error_msg = f"Error processing message: {str(general_error)[:50]}..."
                self.root.after(0, lambda: update_ui_with_error(error_msg))
        
        def on_error(ws, error):
            # Create a function that captures the error value
            def update_ui_with_ws_error():
                self.status_bar.config(text=f"WebSocket error: {str(error)[:50]}...")
                self.status_label.config(text="Error", foreground="red")
            
            self.root.after(0, update_ui_with_ws_error)
        
        def on_close(ws, close_status_code, close_msg):
            # Create a function that captures the close message
            def update_ui_for_close():
                self.status_bar.config(text=f"WebSocket closed: {close_msg or 'No message'}")
                self.status_label.config(text="Disconnected", foreground="red")
                self.connect_button.config(text="Connect")
                self.connected = False
            
            self.root.after(0, update_ui_for_close)
        
        def on_open(ws):
            # Create a function for UI updates on open
            def update_ui_for_open():
                self.connected = True
                self.status_label.config(text="Connected", foreground="green")
                self.connect_button.config(text="Disconnect")
                self.status_bar.config(text=f"Connected to WebSocket for {self.current_instrument}")
            
            self.root.after(0, update_ui_for_open)
            
            # Subscribe to the instrument
            subscribe_msg = json.dumps({
                "action": "subscribe",
                "instrument": self.current_instrument
            })
            ws.send(subscribe_msg)
        
        # Create WebSocket connection
        self.ws = websocket.WebSocketApp(ws_url,
                                         on_open=on_open,
                                         on_message=on_message,
                                         on_error=on_error,
                                         on_close=on_close)
        
        # Run the WebSocket connection
        self.ws.run_forever()
    
    def on_instrument_selected(self, event):
        if self.connected:
            # If already connected, switch instruments
            instrument = self.instrument_var.get()
            self.status_bar.config(text=f"Switching to {instrument}...")
            
            # Send subscription message
            subscribe_msg = json.dumps({
                "action": "subscribe",
                "instrument": instrument
            })
            
            # Send in a separate thread to avoid blocking the UI
            threading.Thread(target=lambda: self.ws.send(subscribe_msg)).start()
            
            # Update current instrument
            self.current_instrument = instrument
            self.clear_orderbook()
    
    def clear_orderbook(self):
        # Clear the treeview
        for item in self.orderbook_tree.get_children():
            self.orderbook_tree.delete(item)
        
        # Reset labels
        self.last_updated_label.config(text="-")
        self.mid_price_label.config(text="-")
    
    def update_orderbook(self, data):
        # Clear existing data
        self.clear_orderbook()
        
        # Update timestamp
        timestamp = datetime.fromtimestamp(data.get('timestamp', 0) / 1000)
        self.last_updated_label.config(text=timestamp.strftime("%H:%M:%S"))
        
        # Extract bid and ask prices
        bids = data.get('bids', {})
        asks = data.get('asks', {})
        
        # Convert string keys to float for sorting
        bid_prices = sorted([float(price) for price in bids.keys()], reverse=True)
        ask_prices = sorted([float(price) for price in asks.keys()])
        
        # Calculate mid price
        if bid_prices and ask_prices:
            mid_price = (bid_prices[0] + ask_prices[0]) / 2
            self.mid_price_label.config(text=f"{mid_price:.8f}")
        
        # Merge and sort all prices
        all_prices = sorted(set(bid_prices + ask_prices), reverse=True)
        
        # Add rows to treeview
        for i, price in enumerate(all_prices):
            price_str = f"{price:.8f}"
            
            # Get quantities, considering they might be string keys
            bid_qty = ""
            ask_qty = ""
            
            # Try exact match first
            if price_str in bids:
                bid_qty = bids[price_str]
            # If not found, try to find the float key
            elif price in bids:
                bid_qty = bids[price]
                
            if price_str in asks:
                ask_qty = asks[price_str]
            elif price in asks:
                ask_qty = asks[price]
            
            # Format quantities
            bid_display = f"{float(bid_qty):.2f}" if bid_qty else ""
            ask_display = f"{float(ask_qty):.2f}" if ask_qty else ""
            
            # Add row to treeview
            row_id = self.orderbook_tree.insert("", tk.END, values=(bid_display, price_str, ask_display))
            
            # Highlight best bid and ask
            if bid_prices and price == bid_prices[0]:
                self.orderbook_tree.item(row_id, tags=("best_bid",))
            elif ask_prices and price == ask_prices[0]:
                self.orderbook_tree.item(row_id, tags=("best_ask",))
        
        # Configure tag colors
        self.orderbook_tree.tag_configure("best_bid", background="#d4f7d4")
        self.orderbook_tree.tag_configure("best_ask", background="#f7d4d4")
        
        # Update status
        self.status_bar.config(text=f"Updated orderbook for {self.current_instrument} with "
                                     f"{len(bid_prices)} bids and {len(ask_prices)} asks")


if __name__ == "__main__":
    root = tk.Tk()
    app = OrderbookApp(root)
    root.mainloop()