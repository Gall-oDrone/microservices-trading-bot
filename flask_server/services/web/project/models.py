from datetime import datetime
from flask_sqlalchemy import SQLAlchemy

db = SQLAlchemy()

class User(db.Model):
    __tablename__ = "users"

    id = db.Column(db.Integer, primary_key=True)
    email = db.Column(db.String(128), unique=True, nullable=False)
    active = db.Column(db.Boolean(), default=True, nullable=False)

    def __init__(self, email):
        self.email = email

SOURCE_TYPE = ['news', 'press_release', 'sec_filling']

class New(db.Model):
    __tablename__ = "news"

    id = db.Column(db.Integer, primary_key=True, autoincrement=True)
    company = db.Column(db.String(), nullable=False)
    ticker = db.Column(db.String(), nullable=False)
    headline = db.Column(db.String(), nullable=False)
    site = db.Column(db.String(), nullable=False)
    description = db.Column(db.String(), nullable=False)
    published_date = db.Column(db.DateTime, nullable=True)
    sentiment = db.Column(db.String(), nullable=False)

    def __repr__(self):
        return f"<Company {self.id} >"
