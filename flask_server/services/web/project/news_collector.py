import time
from bs4 import BeautifulSoup
from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.chrome.service import Service
from webdriver_manager.chrome import ChromeDriverManager
from selenium.webdriver.common.by import By

DRIVER_PATH= "/chromedriver/chromedriver"
options = webdriver.ChromeOptions()
options.add_argument("--headless")
options.add_argument("--no-sandbox")
options.add_argument('--start-maximized')
options.add_argument('--disable-extensions')
service = Service(ChromeDriverManager().install())
driver =  webdriver.Chrome(service= Service(ChromeDriverManager().install() ), options = options)
   
#COMPANY={ticker:['JPM'], name=["J.P. Morgan"]}

# e.g. https://finance.yahoo.com/quote/JPM?p=JPM&.tsrc=fin-srch
def extract_info(url:str, xpath:str, **kwargs):
    try:
        driver.get(url)
        time.sleep(3)
        news_headline = driver.find_element(By.XPATH, xpath)
        print(f'New headline: {news_headline}')
        driver.close()
        return news_headline
    except Exception as e:
        print('error during web scraping: ', e)


