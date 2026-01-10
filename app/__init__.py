from flask import Flask
from pathlib import Path
from flask_login import LoginManager
from flask_sqlalchemy import SQLAlchemy
from flask_migrate import Migrate

BASE_DIR = Path(__file__).resolve().parent.parent
DATA_DIR = BASE_DIR / 'data'

db = SQLAlchemy()
migrate = Migrate()
login_manager = LoginManager()
login_manager.login_view = 'auth.login'


def create_app():
    app = Flask(__name__)

    # Core config
    app.config['SECRET_KEY'] = 'change-me'
    app.config['DATA_DIR'] = str(DATA_DIR)

    # SQLite file in the project root (poolclub.db)
    app.config['SQLALCHEMY_DATABASE_URI'] = 'sqlite:///' + str(BASE_DIR / 'poolclub.db')
    app.config['SQLALCHEMY_TRACK_MODIFICATIONS'] = False

    # Init extensions
    db.init_app(app)
    migrate.init_app(app, db)
    login_manager.init_app(app)

    # Late imports to avoid circulars
    from .model import get_user_by_id
    from .routes import main
    from .auth import auth

    @login_manager.user_loader
    def load_user(user_id: str):
        return get_user_by_id(user_id)

    app.register_blueprint(main)
    app.register_blueprint(auth)

    return app