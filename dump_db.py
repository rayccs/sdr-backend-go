import sqlite3
import sys

db_path = "C:/Users/josed/Proyectos/Nodepath/services/SDR_COGNITIVO_B2B/sdr-backend-go/sdr_backend.db"
conn = sqlite3.connect(db_path)
conn.row_factory = sqlite3.Row
cursor = conn.cursor()

def dump_table(table_name):
    print(f"\n--- {table_name} ---")
    cursor.execute(f"PRAGMA table_info({table_name});")
    columns = [col['name'] for col in cursor.fetchall()]
    print(" | ".join(columns))
    
    cursor.execute(f"SELECT * FROM {table_name};")
    rows = cursor.fetchall()
    for row in rows:
        print(" | ".join([str(row[col]) for col in columns]))

dump_table("users")
dump_table("company_configs")

conn.close()
