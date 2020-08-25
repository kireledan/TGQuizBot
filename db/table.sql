CREATE TABLE connected_users
		(
		  chatid bigint NOT NULL,
		  nextquestiontime timestamp with time zone,
		  questioninterval INTERVAL,
		);
		
ALTER TABLE connected_users ADD CONSTRAINT chatid PRIMARY KEY (chatid);


ALTER TABLE connected_users
ADD COLUMN questionsAsked int;

ALTER TABLE connected_users
ADD COLUMN questionsCorrect int;

ALTER TABLE connected_users
ADD COLUMN questionsSent int;

ALTER TABLE connected_users
ADD COLUMN QuizSection TEXT;
UPDATE connected_users SET quizsection = 'network+'; 


ALTER TABLE connected_users
ADD COLUMN LastAnsweredTime timestamp with time zone;
UPDATE connected_users SET LastAnsweredTime = ; 

ALTER TABLE connected_users
ADD COLUMN Username TEXT;
UPDATE connected_users SET username = 'unknown'; 


CREATE TABLE Questions(
    QuestionText    text primary key,
    Choices    		text[],
    Answers           int[],
	ID				text,
	Tags			text[]
);

ALTER TABLE connected_users
ADD COLUMN QuestionsAsked text[];