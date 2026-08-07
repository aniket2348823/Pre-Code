"""Clean snippet: parameterized query — no string interpolation."""
import sqlite3


def get_user(db, user_id):
    cursor = db.cursor()
    cursor.execute("SELECT * FROM users WHERE id = ?", (user_id,))
    return cursor.fetchone()
