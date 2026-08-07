"""AI-generated snippet: SQL injection via f-string interpolation."""
import sqlite3


def get_user(db, user_id):
    cursor = db.cursor()
    cursor.execute(f"SELECT * FROM users WHERE id = {user_id}")
    return cursor.fetchone()
