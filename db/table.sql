CREATE TABLE connected_users
		(
		  chatid bigint NOT NULL,
		  nextquestiontime timestamp with time zone,
		  questioninterval INTERVAL
		);
		
ALTER TABLE connected_users ADD CONSTRAINT chatid PRIMARY KEY (chatid);


ALTER TABLE connected_users
ADD COLUMN questionsAsked int constraint;

ALTER TABLE connected_users
ADD COLUMN questionsCorrect int constraint;
