import dash
from dash import dcc, html, dash_table
from dash.dependencies import Input, Output, State
import plotly.graph_objects as go
import plotly.express as px
import pandas as pd
import numpy as np
import yfinance as yf
from py_vollib.black_scholes import black_scholes
from py_vollib.black_scholes.greeks.analytical import delta, gamma, vega, theta, rho
import datetime as dt

# Initialize the Dash app
app = dash.Dash(__name__, external_stylesheets=['https://stackpath.bootstrapcdn.com/bootstrap/4.5.0/css/bootstrap.min.css'])

# App layout
app.layout = html.Div([
    # Header
    html.Div([
        html.H1('Options Analytics Dashboard', className='display-4 text-center mb-4'),
        html.Div([
            html.Div([
                html.Label('Ticker Symbol:'),
                dcc.Input(id='ticker-input', value='SPY', type='text', className='form-control')
            ], className='col-md-3'),
            html.Div([
                html.Label('Expiration Date:'),
                dcc.Dropdown(id='expiry-dropdown', className='form-control')
            ], className='col-md-3'),
            html.Div([
                html.Button('Refresh Data', id='refresh-button', className='btn btn-primary mt-4')
            ], className='col-md-3'),
            html.Div([
                dcc.Checklist(
                    id='view-mode',
                    options=[{'label': 'Dark Mode', 'value': 'dark'}],
                    value=[],
                    className='mt-4'
                )
            ], className='col-md-3'),
        ], className='row mb-4'),
    ], className='container-fluid p-5 bg-light'),
    
    # Main Dashboard Content
    html.Div([
        # Market Overview Section
        html.Div([
            html.H3('Market Overview', className='mb-4'),
            html.Div([
                html.Div([
                    html.H5('Price & IV History'),
                    dcc.Graph(id='price-iv-chart', style={'height': '400px'})
                ], className='col-md-6'),
                html.Div([
                    html.H5('Volatility Surface'),
                    dcc.Graph(id='vol-surface-chart', style={'height': '400px'})
                ], className='col-md-6'),
            ], className='row mb-4'),
            
            html.Div([
                html.Div([
                    html.H5('Put/Call Ratio'),
                    dcc.Graph(id='put-call-ratio', style={'height': '300px'})
                ], className='col-md-4'),
                html.Div([
                    html.H5('IV Rank'),
                    dcc.Graph(id='iv-rank-chart', style={'height': '300px'})
                ], className='col-md-4'),
                html.Div([
                    html.H5('Option Volume'),
                    dcc.Graph(id='volume-chart', style={'height': '300px'})
                ], className='col-md-4'),
            ], className='row mb-4'),
        ], className='container-fluid mb-5'),
        
        # Options Chain Section
        html.Div([
            html.H3('Options Chain Analysis', className='mb-4'),
            html.Div([
                dash_table.DataTable(
                    id='options-chain-table',
                    style_table={'overflowX': 'auto'},
                    style_cell={
                        'fontSize': '12px',
                        'font-family': 'Arial',
                        'textAlign': 'center'
                    },
                    style_header={
                        'backgroundColor': 'rgb(230, 230, 230)',
                        'fontWeight': 'bold'
                    },
                    style_data_conditional=[
                        {
                            'if': {'column_id': 'strike_price'},
                            'fontWeight': 'bold',
                            'backgroundColor': 'rgb(240, 240, 240)'
                        }
                    ]
                )
            ], className='row mb-4'),
        ], className='container-fluid mb-5'),
        
        # Greeks Analysis Section
        html.Div([
            html.H3('Greeks Analysis', className='mb-4'),
            html.Div([
                html.Div([
                    html.H5('Delta Exposure by Strike'),
                    dcc.Graph(id='delta-exposure-chart', style={'height': '400px'})
                ], className='col-md-6'),
                html.Div([
                    html.H5('Gamma Exposure by Strike'),
                    dcc.Graph(id='gamma-exposure-chart', style={'height': '400px'})
                ], className='col-md-6'),
            ], className='row mb-4'),
            
            html.Div([
                html.Div([
                    html.H5('Theta Decay Profile'),
                    dcc.Graph(id='theta-decay-chart', style={'height': '400px'})
                ], className='col-md-6'),
                html.Div([
                    html.H5('Vega Sensitivity'),
                    dcc.Graph(id='vega-sensitivity-chart', style={'height': '400px'})
                ], className='col-md-6'),
            ], className='row mb-4'),
        ], className='container-fluid mb-5'),
        
        # Risk Analysis Section
        html.Div([
            html.H3('Risk Analysis', className='mb-4'),
            html.Div([
                html.Div([
                    html.H5('Profit/Loss Scenarios'),
                    dcc.Graph(id='pnl-chart', style={'height': '400px'})
                ], className='col-md-6'),
                html.Div([
                    html.H5('IV Change Sensitivity'),
                    dcc.Graph(id='iv-sensitivity-chart', style={'height': '400px'})
                ], className='col-md-6'),
            ], className='row mb-4'),
        ], className='container-fluid'),
    ], id='main-content', className='container-fluid p-5'),
    
    # Store component for data
    dcc.Store(id='options-data'),
    dcc.Store(id='historical-data'),
    dcc.Interval(id='auto-refresh', interval=60*1000, n_intervals=0)  # Refresh every minute
])

# Callback to load options chain data
@app.callback(
    [Output('expiry-dropdown', 'options'),
     Output('options-data', 'data'),
     Output('historical-data', 'data')],
    [Input('refresh-button', 'n_clicks'),
     Input('auto-refresh', 'n_intervals')],
    [State('ticker-input', 'value')]
)
def load_options_data(n_clicks, n_intervals, ticker):
    # Get options data
    try:
        stock = yf.Ticker(ticker)
        expirations = stock.options
        
        expiry_options = [{'label': exp, 'value': exp} for exp in expirations]
        
        # Get historical data for IV calculation
        hist_data = stock.history(period='1y')
        
        # Get options chain for each expiration
        all_options = {}
        for exp in expirations:
            try:
                opt = stock.option_chain(exp)
                # Combine calls and puts
                calls = opt.calls.assign(option_type='call')
                puts = opt.puts.assign(option_type='put')
                chain = pd.concat([calls, puts])
                
                # Calculate additional metrics
                underlying_price = stock.info['regularMarketPrice']
                
                # Add to dictionary
                all_options[exp] = chain.to_dict('records')
            except:
                continue
                
        return expiry_options, all_options, hist_data.to_dict('records')
    except Exception as e:
        print(f"Error loading data: {e}")
        return [], {}, {}

# Callback to update options chain table
@app.callback(
    Output('options-chain-table', 'data'),
    Output('options-chain-table', 'columns'),
    Input('expiry-dropdown', 'value'),
    State('options-data', 'data')
)
def update_options_chain(selected_expiry, options_data):
    if not selected_expiry or not options_data or selected_expiry not in options_data:
        return [], []
    
    chain_data = pd.DataFrame(options_data[selected_expiry])
    
    # Format the table
    display_columns = [
        'strike', 'option_type', 'lastPrice', 'bid', 'ask', 'volume', 
        'openInterest', 'impliedVolatility', 'inTheMoney'
    ]
    
    # Rename columns for better display
    column_names = {
        'strike': 'Strike Price',
        'option_type': 'Type',
        'lastPrice': 'Last Price',
        'bid': 'Bid',
        'ask': 'Ask',
        'volume': 'Volume',
        'openInterest': 'Open Interest',
        'impliedVolatility': 'IV',
        'inTheMoney': 'ITM'
    }
    
    # Filter and format data
    table_data = chain_data[display_columns].sort_values(by=['strike', 'option_type'])
    
    # Format implied volatility
    table_data['impliedVolatility'] = table_data['impliedVolatility'].apply(lambda x: f"{x*100:.2f}%")
    
    # Format in-the-money
    table_data['inTheMoney'] = table_data['inTheMoney'].apply(lambda x: '✓' if x else '')
    
    columns = [{"name": column_names.get(i, i), "id": i} for i in display_columns]
    
    return table_data.to_dict('records'), columns

# Callback to update the price & IV chart
@app.callback(
    Output('price-iv-chart', 'figure'),
    Input('ticker-input', 'value'),
    Input('historical-data', 'data'),
    Input('view-mode', 'value')
)
def update_price_iv_chart(ticker, historical_data, view_mode):
    if not historical_data:
        return {}
    
    dark_mode = 'dark' in view_mode
    
    df = pd.DataFrame(historical_data)
    
    # Calculate 30-day historical volatility
    if 'Close' in df.columns:
        df['log_return'] = np.log(df['Close'] / df['Close'].shift(1))
        df['hv_30d'] = df['log_return'].rolling(window=30).std() * np.sqrt(252) * 100
    
    # Create the figure with two y-axes
    fig = go.Figure()
    
    # Add price trace
    fig.add_trace(
        go.Scatter(
            x=df.index,
            y=df['Close'] if 'Close' in df.columns else [],
            name=f'{ticker} Price',
            line=dict(color='blue')
        )
    )
    
    # Add HV trace
    if 'hv_30d' in df.columns:
        fig.add_trace(
            go.Scatter(
                x=df.index,
                y=df['hv_30d'],
                name='30-Day HV (%)',
                line=dict(color='orange'),
                yaxis='y2'
            )
        )
    
    # Set layout
    fig.update_layout(
        title=f'{ticker} Price and Historical Volatility',
        xaxis=dict(title='Date'),
        yaxis=dict(title='Price', side='left', showgrid=False),
        yaxis2=dict(title='Volatility (%)', side='right', overlaying='y', showgrid=False),
        legend=dict(x=0.01, y=0.99),
        template='plotly_dark' if dark_mode else 'plotly_white',
        margin=dict(l=40, r=40, t=40, b=40),
    )
    
    return fig

# Run the app
if __name__ == '__main__':
    app.run_server(debug=True)