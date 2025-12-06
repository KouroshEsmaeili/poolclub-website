from flask import Flask
from pathlib import Path
from flask_login import LoginManager
from .model import get_user_by_id

BASE_DIR = Path(__file__).resolve().parent.parent
DATA_DIR = BASE_DIR / 'data'

login_manager = LoginManager()
login_manager.login_view = 'auth.login'  # endpoint name of our login view


@login_manager.user_loader
def load_user(user_id: str):
    # Flask-Login uses this to reload the user from the session
    return get_user_by_id(user_id)


def create_app():
    app = Flask(__name__)
    app.config['SECRET_KEY'] = 'change-me'
    app.config['DATA_DIR'] = str(DATA_DIR)
    
    # init Flask-Login
    login_manager.init_app(app)

    # blueprints
    from .routes import main
    app.register_blueprint(main)

    from .auth import auth
    app.register_blueprint(auth)

    return app
