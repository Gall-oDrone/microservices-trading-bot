import psycopg2
from psycopg2 import sql
import pandas as pd

def create_connection():
    # Replace these placeholders with your actual database credentials
    '''
    dbname = os.environ['DB_NAME']
    user = os.environ['DB_USER']
    password = os.environ['DB_PASSWORD']
    host = os.environ['DB_HOST']  # Change to your database host if it's not local
    port = os.environ['DB_PORT']
    '''
    dbname = "cryptostocks"
    user = "postgres"
    password = "gallo"
    host = "localhost"  # Change to your database host if it's not local
    port = "5432"  # Change to your database port if it's not the default (5432)

    try:
        connection = psycopg2.connect(
            dbname=dbname,
            user=user,
            password=password,
            host=host,
            port=port
        )
        print("Connection to the database successful!")
        return connection
    except Exception as e:
        print(f"Error: Unable to connect to the database. {e}")
        return None

def create_table(connection):
    try:
        cursor = connection.cursor()
        # Define your table schema here
        create_table_query = """
            CREATE TABLE IF NOT EXISTS historical (
                reference VARCHAR(255),
                book VARCHAR(255),
                date DATE,
                open FLOAT,
                high FLOAT,
                low FLOAT,
                close FLOAT,
                adj_close FLOAT,
                volume BIGINT
            )
        """

        cursor.execute(create_table_query)
        connection.commit()
        print("Table created successfully!")
    except Exception as e:
        print(f"Error: Unable to create the table. {e}")
    cursor.close()

def delete_table(table_name, conn):
    # Create a cursor to execute SQL commands
    cursor = conn.cursor()

    try:
        # Generate the SQL DROP TABLE statement
        drop_table_sql = f"DROP TABLE IF EXISTS {table_name}"

        # Execute the DROP TABLE statement
        cursor.execute(drop_table_sql)

        # Commit the changes to the database
        conn.commit()
        print(f"The table '{table_name}' has been deleted successfully.")
    except Error as e:
        # If an error occurs, print the error message
        print("Error:", e)
        conn.rollback()
    finally:
        # Close the cursor
        cursor.close()

        
def save_to_postgres(row_data, header, conn):
    cursor = conn.cursor()
    # Create a dictionary to map header to data in the current row
    row_dict = dict(zip(header, row_data))

    # Generate the SQL INSERT statement
    insert_sql = "INSERT INTO historical ({}) VALUES ({}) ON CONFLICT DO NOTHING".format(
        ", ".join(row_dict.keys()),
        ", ".join("%s" for _ in range(len(row_dict)))
    )

    try:
        # Execute the SQL query with row_data as the values to be inserted
        cursor.execute(insert_sql, list(row_dict.values()))
        # Commit the changes to the database
        conn.commit()
    except Exception as e:
        # If an error occurs, print the error message and roll back the transaction
        print("error on saving data to pg:", e, "\n row_dict:", row_dict)
        conn.rollback()
    cursor.close()

def get_data(conn, book_names=[]):
    cursor = conn.cursor()
    try:
        if book_names:
            query = sql.SQL("SELECT * FROM historical WHERE book IN {}").format(
            sql.Literal(tuple(book_names))
            )
        else:
            query = sql.SQL("SELECT * FROM historical")

        cursor.execute(query)
        data = cursor.fetchall()

        # Define the column names
        columns = ["ref", "book", "date", "open", "high", "low", "close", "adj_close", "volume"]

        # Create a DataFrame from the retrieved data
        df = pd.DataFrame(data, columns=columns)

        return df

    except Exception as e:
        print(f"Error: Unable to retrieve data from the database. {e}")
        return None
    finally:
        if conn:
            conn.close()
            

def init_db():
    # Connect to the database
    connection = create_connection()
    if not connection:
        return

    # Create the table (if not exists)
    create_table(connection)
    return connection

def close_connection(conn):
    # Close the connection
    conn.close()
    print("Connection closed.")