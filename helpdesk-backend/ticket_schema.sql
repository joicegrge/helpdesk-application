USE helpdesk_db;

-- Ticket categories. The unique key plus INSERT IGNORE makes seeding repeatable.
CREATE TABLE IF NOT EXISTS categories (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name VARCHAR(100) NOT NULL,
  description TEXT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_categories_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Ticket and comment ownership references the existing users table.
CREATE TABLE IF NOT EXISTS tickets (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ticket_number VARCHAR(32) NOT NULL,
  title VARCHAR(255) NOT NULL,
  description TEXT NOT NULL,
  category_id BIGINT UNSIGNED NOT NULL,
  priority ENUM('LOW', 'MEDIUM', 'HIGH', 'CRITICAL') NOT NULL DEFAULT 'MEDIUM',
  status ENUM('OPEN', 'ASSIGNED', 'IN_PROGRESS', 'RESOLVED', 'PENDING_APPROVAL', 'PENDING_RESOLUTION_APPROVAL', 'PENDING_USER_CONFIRMATION', 'CLOSED') NOT NULL DEFAULT 'OPEN',
  assigned_to BIGINT UNSIGNED NULL,
  created_by BIGINT UNSIGNED NOT NULL,
  resolution_description TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_tickets_ticket_number (ticket_number),
  KEY idx_tickets_category_id (category_id),
  KEY idx_tickets_assigned_to (assigned_to),
  KEY idx_tickets_created_by (created_by),
  KEY idx_tickets_status (status),
  CONSTRAINT fk_tickets_category FOREIGN KEY (category_id)
    REFERENCES categories (id) ON UPDATE CASCADE ON DELETE RESTRICT,
  CONSTRAINT fk_tickets_assigned_to FOREIGN KEY (assigned_to)
    REFERENCES users (id) ON UPDATE CASCADE ON DELETE SET NULL,
  CONSTRAINT fk_tickets_created_by FOREIGN KEY (created_by)
    REFERENCES users (id) ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS comments (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  ticket_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  comment_text TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_comments_ticket_id_created_at (ticket_id, created_at),
  KEY idx_comments_user_id (user_id),
  CONSTRAINT fk_comments_ticket FOREIGN KEY (ticket_id)
    REFERENCES tickets (id) ON UPDATE CASCADE ON DELETE CASCADE,
  CONSTRAINT fk_comments_user FOREIGN KEY (user_id)
    REFERENCES users (id) ON UPDATE CASCADE ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO categories (name, description) VALUES
  ('Hardware', 'Physical computers, peripherals, and other equipment.'),
  ('Software', 'Applications, operating systems, and software issues.'),
  ('Network', 'Network connectivity, VPN, and infrastructure issues.'),
  ('Email', 'Email accounts, delivery, and calendar issues.'),
  ('Access Management', 'Account access, permissions, and authentication requests.');
