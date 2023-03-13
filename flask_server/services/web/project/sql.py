from datetime import datetime
from .models import User, db


# create a new user
user = User(email="john@example.com")

# add the user to the database
db.session.add(user)
db.session.commit()