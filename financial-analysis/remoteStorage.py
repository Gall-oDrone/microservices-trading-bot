import os
import boto3
import pandas as pd
from io import StringIO
from botocore.exceptions import ClientError
import json

def create_bucket(bucket_name):
    s3 = boto3.client('s3')
    
    # Check if the bucket name is available
    try:
        response = s3.head_bucket(Bucket=bucket_name)
        print(f"Bucket '{bucket_name}' already exists.")
    except ClientError as e:
        if e.response['Error']['Code'] == '404':
            # Create the bucket if it doesn't exist
            try:
                '''
                response = s3.create_bucket(Bucket=bucket_name, ObjectOwnership='ObjectWriter')
                '''
                s3 = boto3.resource('s3')
                s3.create_bucket(Bucket=bucket_name,ObjectOwnership='ObjectWriter')
                s3.put_public_access_block(Bucket=bucket_name, PublicAccessBlockConfiguration={'BlockPublicAcls': False,'IgnorePublicAcls': False,'BlockPublicPolicy': False,'RestrictPublicBuckets': False})
                s3.put_bucket_acl(ACL='public-read-write',Bucket=bucket_name)
                print(f"Bucket '{bucket_name}' created successfully.")
            except ClientError as e:
                print("Error:", e)
        else:
            print("Error:", e)

def upload_file_to_s3(bucket_name, local_file_path, s3_file_path):
    s3 = boto3.client('s3')

    try:
        s3.upload_file(local_file_path, bucket_name, s3_file_path)
        print(f"File '{local_file_path}' uploaded to '{s3_file_path}' in bucket '{bucket_name}' successfully.")
    except ClientError as e:
        print("Error:", e)

        
def upload_dataframe_to_s3(dataframe, bucket_name, file_name):
    # Convert the DataFrame to CSV format
    csv_buffer = StringIO()
    dataframe.to_csv(csv_buffer, index=False)
    
    # Connect to Amazon S3
    s3_client = boto3.client('s3')
                                                                                  
    # Check if the bucket exists
    try:
        s3_client.head_bucket(Bucket=bucket_name)
    except:
        # If the bucket doesn't exist, create it
        create_bucket(bucket_name)
    
    # Upload the CSV data to the specified S3 bucket
    s3_client.put_object(Bucket=bucket_name, Key=file_name, ACL='public-read', Body=csv_buffer.getvalue(), ContentType='text/csv')
    
    # Set the bucket ACL to allow public read access
    s3_client.put_bucket_acl(Bucket=bucket_name, ACL='public-read')
    
    print(f"Data uploaded to S3 bucket '{bucket_name}' as '{file_name}'")
