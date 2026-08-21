create table users
(
    id                 int auto_increment
        primary key,
    username           varchar(255) null,
    password           varchar(255) null,
    date_registered    datetime     null,
    avatar             varchar(255) null,
    date_last_visit    datetime     null,
    interface_language varchar(50)  null,
    interface_timezone varchar(50)  null,
    interface_font_size decimal(5,2) default 1.00 not null,
    interface_design    varchar(255) null,
    user_status        int default 0 not null,
    archive_date       datetime     null,
    archive_reason     varchar(512) null,
    total_posts        int default 0 not null,
    total_general_posts int default 0 not null,
    signature          text         null,
    editor_type        int          not null default 0,
    do_not_blur        tinyint(1)   not null default 0,
    constraint users_pk_2
        unique (username)
);

INSERT IGNORE INTO users (username, password, date_registered, avatar, date_last_visit, interface_language, interface_timezone, user_status, interface_font_size) VALUES ('guest', null, null, null, null, null, null, 0, 1.00);
UPDATE users SET id = 0 WHERE username = 'guest';
ALTER TABLE users AUTO_INCREMENT = 1;

INSERT IGNORE INTO users (id, username, password, date_registered, avatar, date_last_visit, interface_language, interface_timezone, user_status, interface_font_size) VALUES (1, 'The Nameless One', null, null, null, null, null, null, 0, 1.00);
ALTER TABLE users AUTO_INCREMENT = 2;

create table user_role
(
    user_id int not null,
    role_id int not null,
    constraint user_role_pk
        primary key (user_id, role_id)
);

CREATE TABLE custom_field_config
(
    entity_type VARCHAR(255) NOT NULL,
    config      JSON         NULL,
    PRIMARY KEY (entity_type)
);

-- Indexing remains the same syntax
CREATE INDEX custom_field_config_entity_type_index
    ON custom_field_config (entity_type);

INSERT IGNORE INTO custom_field_config (entity_type, config)
VALUES ('character', '[]');

INSERT IGNORE INTO custom_field_config (entity_type, config)
VALUES ('episode', '[]');

INSERT IGNORE INTO custom_field_config (entity_type, config)
VALUES ('character_profile', '[]');

INSERT IGNORE INTO custom_field_config (entity_type, config)
VALUES ('wanted_character', '[]');


CREATE TABLE global_settings
(
    setting_name    VARCHAR(255) NOT NULL,
    setting_value   VARCHAR(255),
    needs_superuser TINYINT(1)   NOT NULL DEFAULT 0,
    PRIMARY KEY (setting_name)
);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('site_name', 'Site Name', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('domain', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('posts_per_page', '20', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('use_image_uploading', 'n', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_add_faction', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_wanted_for_claims', 'moderated', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_users_create_factions', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_guests_create_factions', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_users_create_claims', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('allow_guests_create_claims', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('visual_navlinks_after_header_panel', 'n', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('auto_archiving_enabled', 'n', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('auto_archiving_show_page_link', 'n', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('auto_archiving_days', '20', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('absence_max_days', '30', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('absence_cooldown_days', '7', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('ai_api_key', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('ai_name', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('ai_model', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('github_token', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('github_owner', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('github_repo', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('github_branch', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('use_rating_system', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('show_content_warnings', 'y', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('site_max_rating', 'L1V1S1', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('blur_content_starting_from_rate', NULL, 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('global_free_format_date_id', NULL, 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('imgbb_api_key', '', 1);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('image_hosting', '', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('user_avatar_width', '0', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('user_avatar_height', '0', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('character_avatar_width', '0', 0);

INSERT IGNORE INTO global_settings (setting_name, setting_value, needs_superuser)
VALUES ('character_avatar_height', '0', 0);

CREATE TABLE categories (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NULL,
    position INT NULL
);

CREATE TABLE subforums (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    category_id INT NULL,
    name VARCHAR(255) NULL,
    description MEDIUMTEXT NULL,
    position INT NULL,
    topic_number INT NULL,
    post_number INT NULL,
    date_last_post DATETIME,
    last_post_topic_id bigint unsigned null,
    last_post_topic_name varchar(255) null,
    last_post_id bigint unsigned null,
    last_post_author_user_name varchar(255) null,
    show_last_topic boolean null,
    constraint subforums_categories_id_fk
        foreign key (category_id) references categories (id)
);

CREATE TABLE topics (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    status INT NOT NULL,
    name VARCHAR(255) NOT NULL,
    type INT NOT NULL,
    date_created DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_post_id BIGINT UNSIGNED NULL,
    date_last_post DATETIME,
    last_post_author_user_id INT NULL,
    post_number INT,
    author_user_id INT NOT NULL,
    subforum_id BIGINT UNSIGNED NULL,
    is_sticky BOOLEAN DEFAULT FALSE NULL,
    is_sticky_first_post BOOLEAN DEFAULT FALSE NULL,
    CONSTRAINT fk_topics_subforum
        FOREIGN KEY (subforum_id) REFERENCES subforums (id) ON DELETE NO ACTION ,
    CONSTRAINT fk_topics_user
        FOREIGN KEY (author_user_id) REFERENCES users (id) ON DELETE NO ACTION,
    CONSTRAINT fk_topics_last_post_user
        FOREIGN KEY (last_post_author_user_id) REFERENCES users (id) ON DELETE NO ACTION
);

create table character_base
		(id      bigint unsigned auto_increment primary key,
		user_id int          null,
		name    varchar(255) null,
		avatar  varchar(255) null,
        topic_id bigint unsigned null,
        character_status int default 2 not null,
        total_posts int default 0 null,
        total_episodes int default 0 null,
        date_last_post datetime null,
        is_archived boolean default false null,
		constraint character_base_users_id_fk
		foreign key (user_id) references users (id),
        constraint character_base_topics_id_fk
		foreign key (topic_id) references topics (id)
		);

create table character_main
(
    entity_id                  int            null,
    field_machine_name         varchar(255)   null,
    field_type                 varchar(50)    null,
    value_int                  int            null,
    value_decimal              decimal(10, 2) null,
    value_string               varchar(255)   null,
    value_text                 text           null,
    value_date                 varchar(255)   null,
    value_free_formatted_date  json           null,
    sort_free_formatted_date   bigint         null
);

create table character_profile_base
		(id      bigint unsigned auto_increment primary key,
		character_id bigint unsigned          null,
        avatar varchar(255) null,
        is_archived boolean null,
        is_mask boolean null,
        mask_name varchar(255) null,
        user_id int null,
        signature text null,
		constraint character_profile_base_character_id_fk
		foreign key (character_id) references character_base (id)  ON DELETE CASCADE
		);

create table character_profile_main
(
    entity_id                  int            null,
    field_machine_name         varchar(255)   null,
    field_type                 varchar(50)    null,
    value_int                  int            null,
    value_decimal              decimal(10, 2) null,
    value_string               varchar(255)   null,
    value_text                 text           null,
    value_date                 varchar(255)   null,
    value_free_formatted_date  json           null,
    sort_free_formatted_date   bigint         null
);

CREATE TABLE posts (
                       id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
                       topic_id BIGINT UNSIGNED NOT NULL,
                       author_user_id INT NOT NULL,
                       guest_name VARCHAR(255) NULL,
                       date_created DATETIME DEFAULT CURRENT_TIMESTAMP,
                       content TEXT NOT NULL,
                       character_profile_id BIGINT UNSIGNED,
                       use_character_profile BOOLEAN DEFAULT FALSE,
                       is_deleted TINYINT NULL,
                       CONSTRAINT fk_posts_topic
                           FOREIGN KEY (topic_id) REFERENCES topics (id) ON DELETE CASCADE,
                       CONSTRAINT fk_posts_user
                           FOREIGN KEY (author_user_id) REFERENCES users (id) ON DELETE CASCADE,
                       CONSTRAINT fk_posts_character_profile
                           FOREIGN KEY (character_profile_id) REFERENCES character_profile_base (id) ON DELETE SET NULL
);

ALTER TABLE topics ADD CONSTRAINT topics_posts_id_fk FOREIGN KEY (last_post_id) REFERENCES posts (id);

create table episode_base
		(id               bigint unsigned auto_increment primary key,
		topic_id          bigint unsigned null,
		name              varchar(255)    null,
		open_to_everyone  boolean         default false not null,
		episode_status    int             default 0 not null,
		rating_set        boolean         default false not null,
		rating_language   int             default 0 not null,
		rating_violence   int             default 0 not null,
		rating_sex        int             default 0 not null,
		constraint episode_base_topics_id_fk
		foreign key (topic_id) references topics (id)
		);

create table episode_main
(
    entity_id                  int            null,
    field_machine_name         varchar(255)   null,
    field_type                 varchar(50)    null,
    value_int                  int            null,
    value_decimal              decimal(10, 2) null,
    value_string               varchar(255)   null,
    value_text                 text           null,
    value_date                 varchar(255)   null,
    value_free_formatted_date  json           null,
    sort_free_formatted_date   bigint         null
);

create table episode_character
		(episode_id bigint unsigned          null,
		character_id bigint unsigned          null,
         foreign key (episode_id) references episode_base (id),
         foreign key (character_id) references character_base (id)
		);

create table episode_mask
		(episode_id bigint unsigned          null,
		mask_id bigint unsigned          null,
         foreign key (episode_id) references episode_base (id),
         foreign key (mask_id) references character_profile_base (id)
		);

create table standard_warnings
(
    id               bigint unsigned not null,
    locale           varchar(10)     not null,
    name             varchar(255)    not null,
    description      text            null,
    rating_language  int             default 0 not null,
    rating_violence  int             default 0 not null,
    rating_sex       int             default 0 not null,
    primary key (id, locale)
);

create table episode_warnings
(
    episode_id bigint unsigned not null,
    warning_id bigint unsigned not null,
    foreign key (episode_id) references episode_base (id)
);

create table user_episode_warnings_consent
(
    episode_id bigint unsigned not null,
    user_id    int             not null,
    primary key (episode_id, user_id),
    foreign key (episode_id) references episode_base (id),
    foreign key (user_id) references users (id)
);

INSERT IGNORE INTO standard_warnings (id, locale, name, description, rating_language, rating_violence, rating_sex) VALUES
( 1, 'en', 'Cannibalism',       'Consumption of the flesh or organs of the same species, whether ritualistic, survival-based, or monstrous.',                                                 0, 2, 0),
( 2, 'en', 'Torture',           'Intentional, prolonged infliction of severe physical or psychological pain on a captive character.',                                                         0, 2, 0),
( 3, 'en', 'Body Horror',       'Severe, unsettling violations of normal anatomy (e.g., parasitic infestation, rapid mutation, skin-crawling transformations).',                              0, 1, 0),
( 4, 'en', 'Self-Harm',         'Depictions of characters intentionally causing physical injury to their own bodies or engaging in active suicidal ideation.',                                0, 1, 0),
( 5, 'en', 'Dismemberment',     'The traumatic loss, amputation, or severing of limbs, fingers, or major body parts during a scene.',                                                        0, 2, 0),
( 6, 'en', 'Animal Cruelty',    'Intentional harm, abuse, or graphic death inflicted upon domestic animals, pets, or innocent wildlife.',                                                     0, 1, 0),
( 7, 'en', 'Child Injury',      'Onscreen physical danger, severe injury, or trauma happening to characters who are minors within the setting.',                                              0, 1, 0),
( 8, 'en', 'Loss of Agency',    'Complete stripping of a character''s free will via magical mind control, heavy drugging, hypnosis, or telepathic hijacking.',                               0, 0, 0),
( 9, 'en', 'Severe Abuse',      'Intimate partner violence, systematic gaslighting, or severe domestic and emotional torment.',                                                               1, 1, 0),
(10, 'en', 'Panic Attacks',     'Visceral, detailed depictions of acute anxiety, hyperventilation, flashbacks, or overwhelming PTSD episodes.',                                              0, 0, 0),
(11, 'en', 'Slavery',           'Depictions of human/humanoid trafficking, forced labor, ownership of sentient beings, or institutional captivity.',                                         0, 1, 0),
(12, 'en', 'Hate Speech / Slurs', 'Explicit, targeted bigotry, whether utilizing real-world terms or highly intense, fictionalized fantasy/sci-fi equivalents.',                            2, 0, 0),
(13, 'en', 'Cult Activity',     'Extreme religious trauma, brainwashing techniques, ritualistic psychological breaking, or coercive group dynamics.',                                        0, 0, 0),
(14, 'en', 'Arachnophobia',     'Heavy focus on spiders, scorpions, swarming insects, or arachnid-based monsters/environments.',                                                             0, 0, 0),
(15, 'en', 'Thalassophobia',    'Deep ocean environments, fear of the abyss, drowning, or massive underwater leviathans.',                                                                   0, 0, 0),
(16, 'en', 'Emetophobia',       'Detailed, sensory descriptions of characters vomiting, nausea, or severe gastrointestinal sickness.',                                                       0, 1, 0);

INSERT IGNORE INTO standard_warnings (id, locale, name, description, rating_language, rating_violence, rating_sex) VALUES
( 1, 'ru', 'Каннибализм',                      'Поедание плоти или органов существ своего же вида (ритуальное, ради выживания или чудовищами).',                                                                         0, 2, 0),
( 2, 'ru', 'Пытки',                            'Умышленное, затяжное причинение сильной физической или психологической боли плененному персонажу.',                                                                      0, 2, 0),
( 3, 'ru', 'Боди-хоррор',                      'Серьезные, пугающие нарушения нормальной анатомии (например, заражение паразитами, стремительные мутации, жуткие трансформации плоти).',                                0, 1, 0),
( 4, 'ru', 'Селфхарм / Самоповреждение',       'Изображение персонажей, намеренно наносящих себе физические увечья, или проявления активных суицидальных мыслей.',                                                      0, 1, 0),
( 5, 'ru', 'Расчленение',                      'Травматическая потеря, ампутация или отсечение конечностей, пальцев или крупных частей тела во время сцены.',                                                           0, 2, 0),
( 6, 'ru', 'Жестокое обращение с животными',   'Умышленный вред, насилие или графическая смерть, причиненные домашним животным, питомцам или невинной дикой природе.',                                                  0, 1, 0),
( 7, 'ru', 'Травмы детей',                     'Физическая опасность на экране, серьезные травмы или издевательства над персонажами, которые являются несовершеннолетними в рамках сеттинга.',                          0, 1, 0),
( 8, 'ru', 'Потеря контроля / Нарушение воли', 'Полное лишение персонажа свободы воли посредством магического контроля разума, сильного воздействия наркотиков, гипноза или телепатического перехвата.',               0, 0, 0),
( 9, 'ru', 'Жестокое обращение / Абьюз',       'Насилие со стороны интимного партнера, систематический газлайтинг или тяжелые домашние и эмоциональные мучения.',                                                       1, 1, 0),
(10, 'ru', 'Панические атаки',                 'Висцеральные, подробные изображения острой тревоги, гипервентиляции, флешбэков или тяжелых эпизодов ПТСР.',                                                            0, 0, 0),
(11, 'ru', 'Рабство',                          'Изображение торговли людьми/гуманоидами, принудительного труда, владения разумными существами или институционального плена.',                                           0, 1, 0),
(12, 'ru', 'Язык вражды / Оскорбления',        'Явная, целенаправленная нетерпимость с использованием реальных терминов или очень интенсивных, вымышленных фэнтезийных/научно-фантастических аналогов.',               2, 0, 0),
(13, 'ru', 'Культы / Секты',                   'Тяжелые религиозные травмы, методы промывания мозгов, ритуальное психологическое подавление или принудительная групповая динамика.',                                    0, 0, 0),
(14, 'ru', 'Арахнофобия',                      'Повышенное внимание к паукам, скорпионам, роящимся насекомым или существам/окружению на основе арахнидов.',                                                             0, 0, 0),
(15, 'ru', 'Талассофобия',                     'Глубоководная среда, страх бездны, утопления или массивных подводных левиафанов.',                                                                                       0, 0, 0),
(16, 'ru', 'Эметофобия',                       'Подробные, сенсорные описания рвоты персонажей, тошноты или тяжелого желудочно-кишечного недомогания.',                                                                 0, 1, 0);

create table global_stats
(
    stat_name varchar(255) null
        primary key,
    stat_value decimal      null,
    stat_secondary varchar(255) null
);

INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_user_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_character_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_episode_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_topic_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_post_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value) VALUES ('total_episode_post_number', 0);
INSERT IGNORE INTO global_stats (stat_name, stat_value, stat_secondary) VALUES ('last_user', 0, '');

create table free_format_date_settings
(
    id               int          auto_increment primary key,
    name             varchar(255) not null,
    free_format_date json         not null,
    constraint free_format_date_settings_name_unique unique (name)
);

INSERT IGNORE INTO free_format_date_settings (name, free_format_date) VALUES (
    'Gregorian Calendar',
    '{"format_strings":["$1 $2, $0"],"placeholders":[{"type":"number","name":"year","position":0,"is_nullable":false,"min_value":-9999,"max_value":9999},{"type":"list","name":"month","position":1,"is_nullable":false,"value_list":["January","February","March","April","May","June","July","August","September","October","November","December"]},{"type":"number","name":"day","position":2,"is_nullable":true,"min_value":1,"max_value":31}]}'
);

INSERT IGNORE INTO free_format_date_settings (name, free_format_date) VALUES (
    'Dragon Age',
    '{"format_strings":["$0 Ancient, $5 of $4","$0 Ancient, $4","$1:$2 $3, $5 of $4","$1:$2 $3, $4"],"placeholders":[{"type":"number","name":"ancient_year","position":0,"is_nullable":true,"min_value":-9999,"max_value":-1},{"type":"number","name":"age_number","position":1,"is_nullable":true,"min_value":1,"max_value":9},{"type":"number","name":"year","position":2,"is_nullable":true,"min_value":0,"max_value":99},{"type":"list","name":"month_and_holidays","position":4,"is_nullable":false,"value_list":["First Day","Wintermarch (Verimensis)","Wintersend","Guardian (Pluitanis)","Drakonis (Nubulis)","Cloudreach (Eluviesta)","Summerday","Bloomingtide (Molioris)","Justinian (Ferventis)","Solace (Solis)","All Soul''s Day","August (Matrinalis)","Kingsway (Parvulis)","Harvestmere (Frumentum)","Satinalia","Firstfall (Umbralis)","Haring (Cassus)"]},{"type":"number","name":"day","position":5,"is_nullable":true,"min_value":1,"max_value":30},{"type":"list","name":"age_name","position":3,"is_nullable":true,"value_list":["Divine","Glory","Towers","Black","Exalted","Steel","Storm","Blessed","Dragon"]}]}'
);

INSERT IGNORE INTO free_format_date_settings (name, free_format_date) VALUES (
    'Forgotten Realms (Dale Reckoning)',
    '{"format_strings":["$1 DR, $2 $3","$1 DR, $2"],"placeholders":[{"type":"number","name":"year","position":1,"is_nullable":false,"min_value":-9999,"max_value":9999},{"type":"list","name":"month_or_holiday","position":2,"is_nullable":false,"value_list":["Hammer","Midwinter","Alturiak","Ches","Tarsakh","Greengrass","Mirtul","Kythorn","Flamerule","Midsummer","Shieldmeet","Eleasis","Eleint","Highharvestide","Marpenoth","Uktar","Feast of the Moon ","Nightal"]},{"type":"number","name":"day","position":3,"is_nullable":true,"min_value":1,"max_value":30}]}'
);

create table factions
(
    id                               int          auto_increment  primary key,
    name                             varchar(255) not null,
    parent_id                        int          null,
    level                            int          not null,
    description                      text         null,
    icon                             varchar(255) null,
    show_on_profile                  boolean      not null,
    can_be_multiple                  bool default FALSE null,
    root_id                          int          null,
    faction_status                   int default 2 not null,
    free_format_date_id              int          null,
    constraint fk_factions_free_format_date foreign key (free_format_date_id) references free_format_date_settings (id) on delete set null
);

create table character_faction
(
    character_id bigint unsigned null,
    faction_id   int             null,
    constraint character_faction_character_base_id_fk
        foreign key (character_id) references character_base (id),
    constraint character_faction_factions_id_fk
        foreign key (faction_id) references factions (id)
);

create table character_flattened
(
    entity_id int primary key
);

create table character_profile_flattened
(
    entity_id int primary key
);

create table episode_flattened
(
    entity_id int primary key
);

create table roles
(
    id   int auto_increment
        primary key,
    name varchar(255) null
);

INSERT IGNORE INTO roles (id, name) VALUES (1, 'guest');

INSERT IGNORE INTO roles (id, name) VALUES (2, 'user');

INSERT IGNORE INTO roles (id, name) VALUES (3, 'admin');

create table role_permission
(
    role_id    int          null,
    type       int          default 0,
    permission varchar(255) null,
    constraint role_permission_pk
        primary key (role_id, permission),
    constraint role_permission_roles_id_fk
        foreign key (role_id) references roles (id)
);

-- Default permissions for 'user' role (ID 2)
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 2, '/notifications/unread');
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 2, '/notifications/dismiss/:id');

-- Default permissions for 'admin' role (ID 3)
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 3, '/permission-matrix/get');
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 3, '/permission-matrix/update');
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 3, '/template/:type/update');
INSERT IGNORE INTO role_permission (type, role_id, permission) VALUES (0, 3, '/character/accept/:id');

CREATE TABLE notifications (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id INT NOT NULL,
    type VARCHAR(50) NOT NULL,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    data JSON NULL,
    date_created DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_read BOOLEAN DEFAULT FALSE,
    CONSTRAINT fk_notifications_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

create table user_notification_setting
(
    user_id           int          not null,
    notification_type varchar(50)  not null,
    disable_toast     boolean      not null default false,
    disable_sound     boolean      not null default false,
    disable_all       boolean      not null default false,
    primary key (user_id, notification_type),
    constraint fk_user_notification_setting_user
        foreign key (user_id) references users (id) on delete cascade
);

create table recovery_codes
(
    id            int auto_increment primary key,
    user_id       int          not null,
    recovery_code varchar(255) not null,
    security_code varchar(64)  null,
    date_used     datetime     null,
    constraint fk_recovery_codes_user foreign key (user_id) references users (id) on delete cascade
);

create table private_keys
(
    id               int auto_increment primary key,
    user_id          int          not null,
    private_key      varchar(2000) not null,
    salt             varchar(255) not null,
    iv               varchar(255) not null,
    recovery_code_id int          null,
    is_active        boolean      not null default false,
    constraint fk_private_keys_user foreign key (user_id) references users (id) on delete cascade,
    constraint fk_private_keys_recovery_code foreign key (recovery_code_id) references recovery_codes (id) on delete set null
);

create table direct_chats
(
    id         int auto_increment primary key,
    start_date datetime not null,
    status     int      not null default 0
);

create table direct_chat_users
(
    direct_chat_id          int      not null,
    user_id                 int      not null,
    last_read_message_id    int      null,
    unread_count            int      not null default 0,
    chat_blocked_since_date datetime null,
    constraint direct_chat_users_pk primary key (direct_chat_id, user_id),
    constraint fk_direct_chat_users_chat foreign key (direct_chat_id) references direct_chats (id) on delete cascade,
    constraint fk_direct_chat_users_user foreign key (user_id) references users (id) on delete cascade
);

create table direct_chat_messages
(
    id            int auto_increment primary key,
    chat_id       int      not null,
    user_id       int      not null,
    date_send     datetime not null,
    date_received datetime null,
    ciphertext       text not null,
    iv               varchar(24) not null,
    key_author       text not null,
    key_receiver     text not null,
    constraint fk_direct_chat_messages_chat foreign key (chat_id) references direct_chats (id) on delete cascade,
    constraint fk_direct_chat_messages_user foreign key (user_id) references users (id) on delete cascade
);

ALTER TABLE direct_chat_users
    ADD CONSTRAINT fk_direct_chat_users_last_message
        FOREIGN KEY (last_read_message_id) REFERENCES direct_chat_messages (id) ON DELETE SET NULL;

create table public_keys
(
    id         int auto_increment primary key,
    user_id    int          not null,
    public_key varchar(512) not null,
    constraint fk_public_keys_user foreign key (user_id) references users (id) on delete cascade
);

create table images
(
    id            int auto_increment primary key,
    url           varchar(512) not null,
    thumbnail_url varchar(512) null,
    user_id       int          not null,
    date_created  datetime     default current_timestamp,
    delete_url    varchar(512) null,
    constraint fk_images_user foreign key (user_id) references users (id) on delete cascade
);

create table user_topic_view
(
    user_id   int             not null,
    topic_id  bigint unsigned not null,
    post_id   bigint unsigned null,
    view_date datetime        default current_timestamp,
    primary key (user_id, topic_id),
    constraint fk_user_topic_view_user foreign key (user_id) references users (id) on delete cascade,
    constraint fk_user_topic_view_topic foreign key (topic_id) references topics (id) on delete cascade
);

create table character_claim
(
    id              int auto_increment primary key,
    name            varchar(255) not null,
    description     text         null,
    is_claimed                 boolean      default false not null,
    claim_record_id            int          null,
    can_change_name            boolean      default false not null,
    show_only_with_active_claim boolean     default false not null
);

create table claim_record
(
    id                     int auto_increment primary key,
    claim_id               int          not null,
    user_id                int          null,
    guest_hash             varchar(255) null,
    is_guest               boolean      default false not null,
    claim_date             datetime     not null,
    claim_expiration_date  datetime     null,
    character_id                        bigint unsigned null,
    claim_created_with_character_sheet  boolean null,
    constraint fk_claim_record_claim      foreign key (claim_id) references character_claim (id) on delete cascade,
    constraint fk_claim_record_user       foreign key (user_id) references users (id) on delete set null,
    constraint fk_claim_record_character  foreign key (character_id) references character_base (id) on delete set null
);

ALTER TABLE character_claim
    ADD CONSTRAINT fk_character_claim_record
        FOREIGN KEY (claim_record_id) REFERENCES claim_record (id) ON DELETE SET NULL;

create table character_claim_faction
(
    character_claim_id int null,
    faction_id         int null,
    constraint character_claim_faction_claim_id_fk
        foreign key (character_claim_id) references character_claim (id),
    constraint character_claim_faction_factions_id_fk
        foreign key (faction_id) references factions (id)
);

create table wanted_character_base
(
    id                 int auto_increment primary key,
    name               varchar(255) not null,
    is_claimed         boolean      default false not null,
    author_user_id     int          not null,
    date_created       datetime     default current_timestamp,
    character_claim_id int             null,
    is_deleted              boolean         null,
    topic_id                bigint unsigned null,
    wanted_character_status int             default 0 not null,
    constraint fk_wanted_character_author foreign key (author_user_id) references users (id) on delete cascade,
    constraint fk_wanted_character_claim  foreign key (character_claim_id) references character_claim (id) on delete set null,
    constraint fk_wanted_character_topic  foreign key (topic_id) references topics (id) on delete set null
);

create table wanted_character_main
(
    entity_id                  int            null,
    field_machine_name         varchar(255)   null,
    field_type                 varchar(50)    null,
    value_int                  int            null,
    value_decimal              decimal(10, 2) null,
    value_string               varchar(255)   null,
    value_text                 text           null,
    value_date                 varchar(255)   null,
    value_free_formatted_date  json           null,
    sort_free_formatted_date   bigint         null
);

create table wanted_character_flattened
(
    entity_id int primary key
);

create table wanted_character_faction
(
    wanted_character_id int null,
    faction_id          int null,
    constraint wanted_character_faction_wanted_character_base_id_fk
        foreign key (wanted_character_id) references wanted_character_base (id),
    constraint wanted_character_faction_factions_id_fk
        foreign key (faction_id) references factions (id)
);

create table wanted_character_relations
(
    wanted_character_id   int not null,
    relation_character_id bigint unsigned not null,
    constraint fk_wcr_wanted_character foreign key (wanted_character_id) references wanted_character_base (id) on delete cascade,
    constraint fk_wcr_character        foreign key (relation_character_id) references character_base (id) on delete cascade
);

create table widget_types
(
    id              int auto_increment primary key,
    name            varchar(255) not null,
    config_template text         null,
    func            varchar(255) not null,
    constraint widget_types_name_unique unique (name)
);

INSERT IGNORE INTO widget_types (name, config_template, func) VALUES ('last_post', '{"topic_id": {"type": "int"}}', 'WidgetLastPost');
INSERT IGNORE INTO widget_types (name, config_template, func) VALUES ('random_entities', '{"number": {"type": "int"}, "entity_type": {"type": "string", "values": ["wanted_character", "character"]}, "entity_field_1": {"type": "string", "endpoint": "entity/fields/:entity_type", "can_empty": true}, "entity_field_1_width": {"type": "int", "can_empty": true}, "entity_field_1_height": {"type": "int", "can_empty": true}, "entity_field_2": {"type": "string", "endpoint": "entity/fields/:entity_type", "can_empty": true}, "entity_field_2_width": {"type": "int", "can_empty": true}, "entity_field_2_height": {"type": "int", "can_empty": true}, "filters": {"type": "filters", "can_empty": true}}', 'WidgetRandomEntities');

create table widgets
(
    id          int auto_increment primary key,
    name        varchar(255) not null,
    template_id int          not null,
    config      text         null,
    constraint widgets_widget_types_id_fk
        foreign key (template_id) references widget_types (id)
);

create table widget_panels
(
    `key`     varchar(255) not null primary key,
    content   text         null,
    is_hidden boolean      not null default false
);

INSERT IGNORE INTO widget_panels (`key`, content, is_hidden) VALUES ('header', NULL, false);
INSERT IGNORE INTO widget_panels (`key`, content, is_hidden) VALUES ('footer', NULL, false);

CREATE TABLE static_files
(
    file_name         varchar(255) not null primary key,
    file_created_date datetime     not null,
    file_type         varchar(255) null
);

INSERT IGNORE INTO static_files (file_name, file_created_date, file_type) VALUES ('favicon.ico', '2026-03-01 00:00:00', 'favicon.ico');
INSERT IGNORE INTO static_files (file_name, file_created_date, file_type) VALUES ('custom_style.css', '2026-03-31 00:00:00', 'custom_style.css');
INSERT IGNORE INTO static_files (file_name, file_created_date, file_type) VALUES ('main_style.css', '2026-03-01 00:00:00', 'main_style.css');

CREATE TABLE design_variations
(
    id         int auto_increment primary key,
    class_name varchar(255) null,
    name       varchar(255) null
);
CREATE TABLE additional_navlinks
(
    id     int auto_increment primary key,
    title  varchar(255) not null,
    type   int          not null default 0,
    config      json         null,
    is_inactive boolean      not null default false
);

CREATE TABLE role_navlink
(
    role_id    int not null,
    navlink_id int not null,
    primary key (role_id, navlink_id)
);

CREATE TABLE features
(
    `key`       varchar(255) not null primary key,
    is_active boolean      not null default false
);

INSERT IGNORE INTO features (`key`, is_active) VALUES ('currency', false);
INSERT IGNORE INTO features (`key`, is_active) VALUES ('post_top', false);

CREATE TABLE currency_income_types
(
    `key`       varchar(255) not null primary key,
    amount    int          not null default 0,
    is_active boolean      not null default false
);

INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_game_post', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_wanted_character', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_new_character', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_100_general_posts', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_500_general_posts', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_1000_general_posts', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_100_game_posts', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_500_game_posts', 1, false);
INSERT IGNORE INTO currency_income_types (`key`, amount, is_active) VALUES ('currency_income_1000_game_posts', 1, false);

CREATE TABLE currency_spend_types
(
    `key`     varchar(255) not null primary key,
    amount    int          not null default 0,
    is_active boolean      not null default false
);

INSERT IGNORE INTO currency_spend_types (`key`, amount, is_active) VALUES ('currency_spend_auto_archiving_immunity', 1, false);

CREATE TABLE currency_settings
(
    id            int          not null default 1,
    currency_name varchar(255) null,
    icon_url      varchar(255) null,
    primary key (id)
);

INSERT IGNORE INTO currency_settings (id, currency_name, icon_url) VALUES (1, null, null);

CREATE TABLE currency_user_account
(
    user_id int not null primary key,
    amount  int not null default 0
);

CREATE TABLE currency_user_transactions
(
    id              int          not null auto_increment primary key,
    user_id         int          not null,
    type            tinyint      not null,
    amount          int          not null,
    datetime        datetime     not null,
    status          int          not null default 0,
    income_type_key varchar(255) null,
    metadata        json         null
);

CREATE TABLE post_tops
(
    id         int          not null auto_increment primary key,
    name       varchar(255) not null,
    user_count int          not null default 0,
    days       int          null,
    is_monthly tinyint(1)   not null default 0,
    is_open    tinyint(1)   not null default 0,
    start_date date         null
);

create table reactions
(
    id        int          not null auto_increment primary key,
    url       varchar(255) not null,
    is_active boolean      not null default true
);

create table smile_category
(
    id   int          not null auto_increment primary key,
    name varchar(100) not null
);

create table smiles
(
    id          int          not null auto_increment primary key,
    text_form   varchar(50)  null,
    url         varchar(255) not null,
    category_id int          null,
    constraint fk_smiles_category foreign key (category_id) references smile_category (id) on delete set null
);

create table lore_pages
(
    topic_id  bigint unsigned not null,
    post_id   bigint unsigned not null,
    name      varchar(255)    not null,
    is_hidden boolean         not null default false,
    position  int             not null default 0,
    primary key (topic_id, post_id),
    constraint fk_lore_pages_topic foreign key (topic_id) references topics (id) on delete cascade,
    constraint fk_lore_pages_post  foreign key (post_id)  references posts (id)  on delete cascade
);

create table post_reaction
(
    post_id     bigint unsigned not null,
    reaction_id int             not null,
    user_id     int             not null,
    date_sent   datetime        not null default current_timestamp,
    primary key (post_id, reaction_id, user_id),
    constraint fk_post_reaction_post     foreign key (post_id)     references posts (id)     on delete cascade,
    constraint fk_post_reaction_reaction foreign key (reaction_id) references reactions (id) on delete cascade,
    constraint fk_post_reaction_user     foreign key (user_id)     references users (id)     on delete cascade
);

create table sonic_ingest_cursor
(
    bucket        varchar(64) not null,
    last_id       bigint      not null,
    date_ingested datetime    not null default current_timestamp,
    primary key (bucket)
);

create table qdrant_ingest_cursor
(
    bucket        varchar(64) not null,
    last_id       bigint      not null,
    date_ingested datetime    not null default current_timestamp,
    primary key (bucket)
);

CREATE TABLE auto_archiving_immunity
(
    id           INT AUTO_INCREMENT PRIMARY KEY,
    character_id BIGINT UNSIGNED NOT NULL,
    start_date   DATETIME        NOT NULL,
    end_date     DATETIME        NOT NULL,
    reason       VARCHAR(255)    NOT NULL,
    CONSTRAINT fk_auto_archiving_immunity_character FOREIGN KEY (character_id) REFERENCES character_base (id) ON DELETE CASCADE
);

CREATE TABLE archiving_warning_log
(
    character_id BIGINT UNSIGNED NOT NULL,
    days_warning INT             NOT NULL,
    base_date    DATE            NOT NULL,
    sent_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (character_id, days_warning, base_date),
    CONSTRAINT fk_archiving_warning_log_character FOREIGN KEY (character_id) REFERENCES character_base (id) ON DELETE CASCADE
);

CREATE TABLE pending_user_refresh
(
    user_id    INT      NOT NULL PRIMARY KEY,
    queued_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_pending_user_refresh_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE TABLE design_drafts
(
    id               INT AUTO_INCREMENT PRIMARY KEY,
    name             VARCHAR(32)  NOT NULL,
    session_key      VARCHAR(12)  NOT NULL,
    date_created     DATETIME     NOT NULL,
    date_last_changed DATETIME    NOT NULL,
    main_css         MEDIUMTEXT   NULL,
    custom_style_css MEDIUMTEXT   NULL,
    CONSTRAINT design_drafts_session_key_unique UNIQUE (session_key)
);

CREATE TABLE absent_users
(
    id                  INT AUTO_INCREMENT PRIMARY KEY,
    user_id             INT        NOT NULL,
    absence_start_date  DATETIME   NOT NULL,
    absence_end_date    DATETIME   NOT NULL,
    is_deleted          TINYINT(1) NOT NULL DEFAULT 0,
    CONSTRAINT fk_absent_users_user FOREIGN KEY (user_id) REFERENCES users (id)
);


create table ai_chat_messages
(
    id           int                               not null auto_increment primary key,
    user_id      int                               not null,
    role         enum('user', 'assistant', 'clear') not null,
    content      text                              not null,
    sources      json                              null,
    date_created datetime                          not null default current_timestamp,
    constraint fk_ai_chat_messages_user foreign key (user_id) references users (id) on delete cascade
);

create table ai_task_queue
(
    id             int                                              not null auto_increment primary key,
    type           varchar(32)                                      not null default 'chat',
    user_id        int                                              null,
    payload        json                                             null,
    status         enum ('pending', 'processing', 'done', 'failed') not null default 'pending',
    retries        int                                              not null default 0,
    error          text                                             null,
    date_created   datetime                                         not null default current_timestamp,
    date_started   datetime                                         null,
    date_completed datetime                                         null,
    index idx_ai_task_queue_status (status, date_created),
    index idx_ai_task_queue_user (user_id),
    constraint fk_ai_task_queue_user foreign key (user_id) references users (id) on delete cascade
);

create table vector_search_bucket_subforum
(
    subforum_id bigint unsigned not null,
    bucket      varchar(64)    not null,
    primary key (subforum_id, bucket),
    constraint fk_vsbs_subforum foreign key (subforum_id) references subforums (id) on delete cascade
);

create table mask_stats
(
    id             bigint unsigned not null auto_increment primary key,
    user_id        int             not null,
    total_episodes int             not null default 0,
    total_posts    int             not null default 0,
    date_last_post datetime        null,
    unique key uq_mask_stats_user (user_id),
    constraint fk_mask_stats_user foreign key (user_id) references users (id) on delete cascade
);

create table faction_settings
(
    id               int          auto_increment primary key,
    level            int          not null,
    human_name       varchar(255) not null,
    parent_faction_id int         null,
    constraint fk_faction_settings_parent foreign key (parent_faction_id) references factions (id)
);

create table external_apps
(
    id      int          auto_increment primary key,
    name    varchar(255) not null,
    api_key varchar(255) not null,
    status  boolean      not null default false,
    user_id int          null,
    constraint fk_external_apps_user foreign key (user_id) references users (id) on delete set null
);

create table external_app_permissions
(
    external_app_id int            not null,
    subforum_id     bigint unsigned not null,
    permission      varchar(255)   not null,
    primary key (external_app_id, subforum_id, permission),
    key idx_eap_subforum_id (subforum_id),
    constraint fk_external_app_permissions_app     foreign key (external_app_id) references external_apps (id) on delete cascade,
    constraint fk_external_app_permissions_subforum foreign key (subforum_id)    references subforums (id)      on delete cascade
);

create table user_data_processing
(
    id                   int          auto_increment primary key,
    date_created         datetime     not null,
    user_id              int          not null,
    status               int          not null default 0,
    original_topic_id    varchar(64)  null,
    original_topic_title varchar(255) null,
    new_topic_id         int          null,
    original_post_count  int          null,
    parsed_post_count    int          null,
    forum_domain         varchar(255) null,
    data_extraction_urls json         null,
    user_character_map   json         null,
    constraint fk_udp_user foreign key (user_id) references users (id) on delete cascade
);

create table user_data_migration
(
    id                         int          auto_increment primary key,
    original_forum_domain      varchar(255) null,
    original_forum_type        int          not null,
    original_topic_id          varchar(64)  null,
    original_post_id           varchar(64)  null,
    original_user_id           varchar(64)  null,
    original_user_name         varchar(255) null,
    original_post_content_html mediumtext   null,
    post_content_bb_parsed     mediumtext   null,
    user_id                    int          null,
    character_id               int          null,
    topic_id                   int          null,
    post_id                    int          null,
    is_published               tinyint(1)   not null default 0,
    date_processed             datetime     null,
    date_published             datetime     null,
    processing_id              int          not null,
    constraint fk_udm_processing foreign key (processing_id) references user_data_processing (id) on delete cascade
);

create table resized_image_cache
(
    id           int           auto_increment primary key,
    original_url varchar(2048) not null,
    width        int           not null,
    height       int           not null,
    resized_url  varchar(2048) not null,
    unique key uq_resized (original_url(191), width, height)
);
create table puzzles
(
    id           int          auto_increment primary key,
    title        varchar(255) not null,
    iframe_code  text         not null,
    date_created datetime     not null default current_timestamp,
    is_public    tinyint(1)   not null default 0,
    is_active    tinyint(1)   not null default 1
);

create table puzzle_achievements
(
    id             int          auto_increment primary key,
    puzzle_id      int          not null,
    user_id        int          not null,
    date           datetime     not null default current_timestamp,
    screenshot_url varchar(2048) not null,
    constraint fk_puzzle_achievements_puzzle foreign key (puzzle_id) references puzzles (id) on delete cascade,
    constraint fk_puzzle_achievements_user   foreign key (user_id)   references users (id)   on delete cascade
);

create table ai_agents
(
    id                int           auto_increment primary key,
    title             varchar(255)  not null,
    short_description varchar(255)  null,
    handler           varchar(255)  not null
);

create table ai_agent_implementation
(
    id        int          auto_increment primary key,
    agent_id  int          not null,
    title     varchar(255) not null,
    config    json         null,
    is_active tinyint(1)   not null default 1,
    constraint fk_ai_agent_implementation_agent foreign key (agent_id) references ai_agents (id) on delete cascade
);

create table game_digest_context
(
    id                int             auto_increment primary key,
    implementation_id int             not null,
    topic_id          bigint unsigned not null,
    context_text      text            null,
    date_updated      datetime        not null default current_timestamp on update current_timestamp,
    unique key uq_gdc_impl_topic (implementation_id, topic_id),
    constraint fk_gdc_implementation foreign key (implementation_id) references ai_agent_implementation (id) on delete cascade
);

INSERT IGNORE INTO ai_agents (id, title, short_description, handler) VALUES (
    1,
    'Game digest',
    'Creates a summary of game events for the given period of time',
    'GameDigest'
);

create table post_drafts
(
    id           int             auto_increment primary key,
    draft_id     varchar(36)     not null,
    user_id      int             not null,
    character_id bigint unsigned  null,
    topic_id     bigint unsigned null,
    date_created datetime        not null default current_timestamp,
    is_manual    tinyint(1)      not null default 0,
    is_published tinyint(1)      not null default 0,
    post_id      bigint unsigned null,
    content      text            null,
    index idx_post_drafts_draft_id (draft_id),
    index idx_post_drafts_user_id (user_id),
    constraint fk_post_drafts_user      foreign key (user_id)      references users (id) on delete cascade,
    constraint fk_post_drafts_character foreign key (character_id) references character_base (id) on delete set null
);
