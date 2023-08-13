from flask import Flask, jsonify
from flask_sqlalchemy import SQLAlchemy
# from .news_collector import extract_info
from .stock_collector import extract_stock
app = Flask(__name__)
app.config.from_object("project.config.Config")
db = SQLAlchemy(app)
db.drop_all()
db.create_all()
db.session.commit()

@app.route("/")
def hello_world():
    return jsonify(hello="world")

@app.route("/news/<string:ticker>")
def test_selenium(ticker='JPM', *args):
    contents = []
    ticker = ticker.upper()
    url = f'https://finance.yahoo.com/'
    url = "https://www.python.org/"
    xpath = "/html/body/div[1]/div/div/div[1]/div/div[1]/div[2]/div[1]/div/div/div/div[1]/div/div/div/div/nav/div/div/div/div[1]/div/a"
    xpath = "/html/body/div[1]/main/section[1]/div[2]/div[1]/div[1]/a"
    xpaht="/html/body/div/header/div/div[1]/a"
    # result = extract_info(url, xpath)
    # print("Results: ", result)
    # contents.append(result.contents) if result else ''
    # print("Contents: ", contents)
    # return {"contents":contents}

@app.route("/stock")
def test_stock():
    extract_stock()