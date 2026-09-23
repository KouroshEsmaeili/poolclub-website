CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email varchar(255) NOT NULL,
    password_hash varchar(255) NOT NULL,
    first_name varchar(100) DEFAULT '',
    last_name varchar(100) DEFAULT '',
    wallet_balance integer NOT NULL DEFAULT 0,
    membership_slug varchar(100),
    membership_name varchar(200),
    membership_expires_at date,
    phone varchar(50) DEFAULT '',
    birthdate varchar(20) DEFAULT '',
    emergency_contact varchar(255) DEFAULT ''
);

CREATE UNIQUE INDEX ix_users_email ON users (email);

CREATE TABLE wallet_transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id integer NOT NULL,
    amount integer NOT NULL,
    type varchar(20) NOT NULL,
    timestamp datetime NOT NULL,
    description varchar(255) DEFAULT '',
    FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE TABLE membership_history (
    id varchar(36) NOT NULL PRIMARY KEY,
    user_id integer NOT NULL,
    plan_slug varchar(100) NOT NULL,
    plan_name varchar(200) NOT NULL,
    purchased_at datetime NOT NULL,
    expires_at date NOT NULL,
    amount integer NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'active',
    FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE TABLE class_enrollments (
    id varchar(36) NOT NULL PRIMARY KEY,
    user_id integer NOT NULL,
    class_slug varchar(100) NOT NULL,
    class_name varchar(200) NOT NULL,
    coach varchar(200),
    time varchar(100),
    price integer NOT NULL,
    enrolled_at datetime NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'active',
    FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE TABLE bookings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id integer NOT NULL,
    date varchar(10) NOT NULL,
    time varchar(5) NOT NULL,
    duration integer NOT NULL,
    type varchar(50) NOT NULL,
    lane integer,
    status varchar(20) NOT NULL DEFAULT 'active',
    FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE TABLE event_registrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id integer,
    event_slug varchar(100) NOT NULL,
    title varchar(255) NOT NULL,
    name varchar(255),
    email varchar(255),
    price integer NOT NULL DEFAULT 0,
    created_at datetime NOT NULL,
    status varchar(20) NOT NULL DEFAULT 'registered',
    FOREIGN KEY (user_id) REFERENCES users (id)
);
