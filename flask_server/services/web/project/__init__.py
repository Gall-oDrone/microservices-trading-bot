from selenium.common.exceptions import TimeoutException, NoSuchElementException
from flask import Flask, jsonify
from bs4 import BeautifulSoup
from flask_sqlalchemy import SQLAlchemy
from .models import User, New
from .news_collector import extract_info
import json

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
    result = extract_info(url, xpath)
    print("Results: ", result)
    contents.append(result.contents) if result else ''
    print("Contents: ", contents)
    return {"contents":contents}


import time
from bs4 import BeautifulSoup
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.chrome.service import Service
from webdriver_manager.chrome import ChromeDriverManager
from selenium.webdriver.firefox.service import Service as FirefoxService
from webdriver_manager.firefox import GeckoDriverManager
from selenium.webdriver.common.by import By


DRIVER_PATH= "/chromedriver/chromedriver"
options = webdriver.ChromeOptions()
options.add_argument("--headless")
options.add_argument("--no-sandbox")
options.add_argument('--start-maximized')
options.add_argument('--disable-extensions')
service = Service(ChromeDriverManager().install())
driver =  webdriver.Chrome(service= Service(ChromeDriverManager().install() ), options = options)
ffdriver = driver = webdriver.Firefox(service=FirefoxService(GeckoDriverManager().install()), options = options)

contents = []
url = f'https://finance.yahoo.com/'
xpath = "/html/body/div[1]/div/div/div[1]/div/div[3]/div[1]/div/div[2]/div/div/div/ul/li[1]/div/div/div[2]/h3/a"

try:
    WebDriverWait(driver,5).until(EC.presence_of_element_located((By.TAG_NAME, "body")))
    try:
        element = driver.find_element(By.XPATH, xpath)
        contents.append(element.text) if element else ''
    except NoSuchElementException as nse:
        print(nse)
        print("-----")
        print(str(nse))
        print("-----")
        print(nse.args)
        print("=====")
except TimeoutException as toe:
    print(toe)
    print("-----")
    print(str(toe))
    print("-----")
    print(toe.args)

print("Contents: ", contents)
driver.close()