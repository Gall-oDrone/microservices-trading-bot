from selenium.common.exceptions import TimeoutException, NoSuchElementException
from selenium.webdriver.common.by import By
from selenium.webdriver.common.action_chains import ActionChains
from selenium.webdriver.common.keys import Keys
from flask import Flask, jsonify
from bs4 import BeautifulSoup
from flask_sqlalchemy import SQLAlchemy
from .models import User, New
from .news_collector import extract_info
import json
import time

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
options.add_argument("--user-agent=Mozilla/5.0 (Macintosh; Intel Mac OS X 10_13_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/73.0.3683.86 Safari/537.36")
options.add_argument("--window-size=1920,1080")
options.add_argument("--disable-extensions")
options.add_argument("--proxy-server='direct://'")
options.add_argument("--proxy-bypass-list=*")
options.add_argument("--start-maximized")
options.add_argument('--headless')
options.add_argument('--disable-gpu')
options.add_argument('--disable-dev-shm-usage')
options.add_argument('--no-sandbox')
options.add_argument('--ignore-certificate-errors')
service = Service(ChromeDriverManager().install())
driver =  webdriver.Chrome(service=service, options=options)
#ffdriver = driver = webdriver.Firefox(service=FirefoxService(GeckoDriverManager().install()), options = options)

contents = []
url = f'https://finance.yahoo.com/lookup'
xpath = "/html/body/div[1]/div/div/div[1]/div/div[3]/div[1]/div/div[2]/div/div/div/ul/li[1]/div/div/div[2]/h3/a"

try:
    WebDriverWait(driver,5).until(EC.presence_of_element_located((By.TAG_NAME, "body")))
    try:
        driver.get(url)
        time.sleep(5)
        # get element 
        #RejectAll= driver.find_element(By.XPATH, '//button[@class="End(1px) H(32px) Lh(n) Va(m) Pos(a) Fl(end) Bdrs(2px) Td(n) Fz(s) D(ib) Bxz(bb) Px(10px) Bd Bgc($linkColor) Bgc($linkActiveColor):h finsrch-btn"]')
        RejectAll= driver.find_element(By.XPATH, '/html/body/div[1]/div/div/div[1]/div/div[3]/div[2]/div/div/div/div/div/div[1]/div/div/div/form/input')
        # create action chain object
        action = ActionChains(driver)
        # click the item
        action.click(on_element = RejectAll)
        # perform the operation
        action.perform()

        time.sleep(5)

        tag ='MSFT'

        SearchBar = driver.find_element(By.ID, "yfin-usr-qry")
        SearchBar.send_keys(tag)
        SearchBar.send_keys(Keys.ENTER)
        time.sleep(5)

        MayBeLaterBtn = driver.find_element(By.XPATH, '//button[@class="Mx(a) Fz(16px) Fw(600) Mt(20px) D(n)--mobp"]')
        action = ActionChains(driver)
        action.click(on_element = MayBeLaterBtn)
        action.perform()
        time.sleep(5)

        Table = driver.find_elements(By.XPATH, '//td[contains(@class, "C($primaryColor) W(51%)") or contains(@class, "Ta(end) Fw(600) Lh(14px)")]')
        TableList =[]

        #Collect all Names and Values
        for value in Table:
            TableList.append(value.text)
            print (value.text)


        time.sleep(100)
        # element = driver.find_element(By.XPATH, xpath)
        # contents.append(element.text) if element else ''
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